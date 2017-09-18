package cloud

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/db"

	"github.com/stretchr/testify/assert"
)

func TestJoin(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	cld.provider.(*fakeProvider).listError = errors.New("listError")
	_, err := joinImpl(*cld)
	assert.EqualError(t, err, "listError")
	cld.provider.(*fakeProvider).listError = nil

	_, err = joinImpl(*cld)
	assert.EqualError(t, err, "no blueprints found")

	cld.conn.Txn(db.BlueprintTable).Run(func(view db.Database) error {
		view.InsertBlueprint()
		return nil
	})
	_, err = joinImpl(*cld)
	assert.EqualError(t, err, "namespace change during a cloud run")

	cld.conn.Txn(db.BlueprintTable).Run(func(view db.Database) error {
		bp, _ := view.GetBlueprint()
		bp.Blueprint.Machines = []blueprint.Machine{{
			Provider: string(FakeAmazon),
			Region:   testRegion,
			Size:     "1",
		}}
		bp.Namespace = "ns"
		view.Commit(bp)
		return nil
	})
	_, err = joinImpl(*cld)
	assert.NoError(t, err)
}

func TestUpdateDBMachines(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	cld.conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "1"
		view.Commit(m)

		m = view.InsertMachine()
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "2"
		m.Status = db.Reconnecting
		view.Commit(m)

		cloudMachines := []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			PublicIP: "1.2.3.4",
			Size:     "2",
		}, {
			Provider: FakeAmazon,
			Region:   testRegion,
			PublicIP: "5.6.7.8",
			Size:     "3",
		}}

		cld.updateDBMachines(view, cloudMachines)

		dbms := scrubID(db.SortMachines(view.SelectFromMachine(nil)))
		assert.Equal(t, []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			PublicIP: "1.2.3.4",
			Status:   db.Reconnecting,
			Size:     "2",
		}, {
			Provider: FakeAmazon,
			Region:   testRegion,
			PublicIP: "5.6.7.8",
			Size:     "3",
		}}, dbms)

		return nil
	})
}

func TestPlanUpdates(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	isConnected = func(s string) bool { return true }

	cld.conn.Txn(db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {

		bp := view.InsertBlueprint()
		bp.Blueprint.Machines = []blueprint.Machine{{
			Provider: string(FakeAmazon),
			Region:   testRegion,
			Size:     "1",
		}, {
			Provider:   string(FakeAmazon),
			Region:     testRegion,
			FloatingIP: "5.6.7.8",
			Size:       "3",
		}}
		view.Commit(bp)

		m := view.InsertMachine()
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "2"
		view.Commit(m)

		m = view.InsertMachine()
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "3"
		m.PublicIP = "1.2.3.4"
		view.Commit(m)

		boot, stop, updateIPs := cld.planUpdates(view)
		assert.Equal(t, []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			DiskSize: 32,
			Size:     "1",
			Status:   db.Booting}}, scrubID(boot))
		assert.Equal(t, []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "2",
			Status:   db.Stopping}}, scrubID(stop))
		assert.Equal(t, []db.Machine{{
			Provider:   FakeAmazon,
			Region:     testRegion,
			Size:       "3",
			PublicIP:   "1.2.3.4",
			FloatingIP: "5.6.7.8",
			Status:     db.Connected}}, scrubID(updateIPs))

		return nil
	})
}

func TestMachineScore(t *testing.T) {
	m := db.Machine{
		Provider: db.Amazon,
		Region:   "us-west-1",
		Size:     "m4.large",
		Role:     db.Master,
		CloudID:  "1",
		DiskSize: 32,
	}

	assert.Equal(t, 0, machineScore(m, m))

	// Floating IP
	m1 := m
	m2 := m
	m1.FloatingIP = "5.6.7.8"
	m2.FloatingIP = m1.FloatingIP
	m2.CloudID = "5"
	assert.Equal(t, 1, machineScore(m1, m2))

	// Wrong ID, but an assigned role.
	m1 = m
	m1.CloudID = "5"
	assert.Equal(t, 2, machineScore(m, m2))

	// Wrong ID, but no Role.
	m1 = m
	m2 = m
	m1.CloudID = "5"
	m1.Role = db.None
	m2.Role = db.None
	assert.Equal(t, 3, machineScore(m1, m2))

	// Role
	m1 = m
	m1.Role = db.Worker
	assert.Equal(t, -1, machineScore(m, m1))
	m1.Role = db.None
	assert.Equal(t, 0, machineScore(m, m1))

	// DiskSize
	m1 = m
	m1.DiskSize = 0
	assert.Equal(t, 0, machineScore(m, m1))
	m1.DiskSize = 64
	assert.Equal(t, -1, machineScore(m, m1))

	// Size
	m1 = m
	m1.Size = "wrong"
	assert.Equal(t, -1, machineScore(m, m1))

	// Preemptible
	m1 = m
	m1.Preemptible = true
	assert.Equal(t, -1, machineScore(m, m1))
}

func TestDesiredMachines(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	adminKey = "bar"

	res := cld.desiredMachines([]blueprint.Machine{{
		Provider: "Google", // Wrong Provider
		Region:   "zone-1",
	}, {
		Provider: string(FakeAmazon),
		Region:   testRegion,
		Role:     "invalid",
	}, {
		Provider:    string(FakeAmazon),
		Region:      testRegion,
		Size:        "m4.lage",
		Preemptible: true,
		FloatingIP:  "1.2.3.4",
		Role:        db.Worker,
		SSHKeys:     []string{"foo"},
	}})
	assert.Equal(t, []db.Machine{{
		Provider:    FakeAmazon,
		Region:      testRegion,
		Size:        "m4.lage",
		Preemptible: true,
		FloatingIP:  "1.2.3.4",
		Role:        db.Worker,
		DiskSize:    defaultDiskSize,
		SSHKeys:     []string{"foo", "bar"}}}, res)
}

func TestConnectionStatus(t *testing.T) {
	isConnected = func(s string) bool { return true }

	assert.Equal(t, db.Connected, connectionStatus(db.Machine{PublicIP: "1.2.3.4"}))
	assert.Equal(t, db.Reconnecting,
		connectionStatus(db.Machine{Status: db.Connected}))

	isConnected = func(s string) bool { return false }
	assert.Equal(t, db.Connecting, connectionStatus(db.Machine{PublicIP: "1.2.3.4"}))
	assert.Equal(t, "", connectionStatus(db.Machine{}))
}

func TestDesiredACLs(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	exp := map[acl.ACL]struct{}{
		{CidrIP: "local", MinPort: 1, MaxPort: 65535}: {},
	}

	// Empty blueprint should have "local" added to it.
	acls := cld.desiredACLs(db.Blueprint{})
	assert.Equal(t, exp, acls)

	// A blueprint with local, shouldn't have it added a second time.
	acls = cld.desiredACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{AdminACL: []string{"local"}},
	})
	assert.Equal(t, exp, acls)

	// Connections that aren't to or from public, shouldn't affect the acls.
	acls = cld.desiredACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{
			Connections: []blueprint.Connection{{
				From:    "foo",
				To:      "bar",
				MinPort: 5,
				MaxPort: 6,
			}},
		},
	})
	assert.Equal(t, exp, acls)

	// Connections from public create an ACL.
	acls = cld.desiredACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{
			Connections: []blueprint.Connection{{
				From:    blueprint.PublicInternetLabel,
				To:      "bar",
				MinPort: 1,
				MaxPort: 2,
			}},
		},
	})
	exp[acl.ACL{CidrIP: "0.0.0.0/0", MinPort: 1, MaxPort: 2}] = struct{}{}
	assert.Equal(t, exp, acls)
}

func TestDefaultRegion(t *testing.T) {
	assert.Equal(t, "foo", defaultRegion("Amazon", "foo"))
	assert.Equal(t, "us-west-1", defaultRegion("Amazon", ""))
	assert.Equal(t, "sfo1", defaultRegion("DigitalOcean", ""))
	assert.Equal(t, "us-east1-b", defaultRegion("Google", ""))
	assert.Equal(t, "", defaultRegion("Vagrant", ""))

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic")
		}
	}()
	defaultRegion("Panic", "")
}

func scrubID(dbms []db.Machine) (res []db.Machine) {
	for _, dbm := range dbms {
		dbm.ID = 0
		res = append(res, dbm)
	}
	return
}
