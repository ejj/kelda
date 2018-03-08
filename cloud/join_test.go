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
	_, err := joinImpl(cld)
	assert.EqualError(t, err, "listError")
	cld.provider.(*fakeProvider).listError = nil

	_, err = joinImpl(cld)
	assert.EqualError(t, err, "no blueprints found")

	cld.conn.Txn(db.BlueprintTable).Run(func(view db.Database) error {
		view.InsertBlueprint()
		return nil
	})
	_, err = joinImpl(cld)
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
	_, err = joinImpl(cld)
	assert.NoError(t, err)
}

func TestSyncDBWithCloud(t *testing.T) {
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

		cld.syncDBWithCloud(view, cloudMachines)

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

func TestSyncDBWithBlueprint(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	adminKey = ""

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
			Role:       db.Worker,
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
		m.Role = db.Worker
		view.Commit(m)

		res := cld.syncDBWithBlueprint(view)
		assert.Equal(t, []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			DiskSize: 32,
			Size:     "1",
			Status:   db.Booting}}, scrubID(res.boot))
		assert.Equal(t, []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "2",
			Status:   db.Stopping}}, scrubID(res.terminate))
		assert.Equal(t, []db.Machine{{
			Provider:   FakeAmazon,
			Region:     testRegion,
			Role:       db.Worker,
			Size:       "3",
			PublicIP:   "1.2.3.4",
			FloatingIP: "5.6.7.8",
			Status:     db.Connected}}, scrubID(res.updateIPs))

		return nil
	})
}

func TestSyncDBWithBlueprintFloatingIP(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	isConnected = func(s string) bool { return false }

	desiredFloatingIP := "floatingIP"
	cld.conn.Txn(db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {

		bp := view.InsertBlueprint()
		bp.Blueprint.Machines = []blueprint.Machine{{
			Role:     db.Master,
			Provider: string(FakeAmazon),
			Region:   testRegion,
		}, {
			Role:       db.Worker,
			Provider:   string(FakeAmazon),
			Region:     testRegion,
			FloatingIP: desiredFloatingIP,
		}}
		view.Commit(bp)

		// When the machine has not yet connected, don't attempt to update
		// floating IPs.
		master := view.InsertMachine()
		master.Provider = FakeAmazon
		master.Role = db.Master
		master.Region = testRegion
		view.Commit(master)

		worker := view.InsertMachine()
		worker.Provider = FakeAmazon
		worker.Region = testRegion
		view.Commit(worker)

		res := cld.syncDBWithBlueprint(view)
		assert.Empty(t, res.boot)
		assert.Empty(t, res.terminate)
		assert.Empty(t, res.updateIPs)

		// Once the machine has a role, update its floating IP.
		worker.Role = db.Worker
		view.Commit(worker)
		res = cld.syncDBWithBlueprint(view)
		assert.Subset(t, scrubID(res.updateIPs), []db.Machine{
			{
				Provider:   FakeAmazon,
				Region:     testRegion,
				Role:       db.Worker,
				FloatingIP: desiredFloatingIP,
			},
		})

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
	assert.Equal(t, 7, machineScore(m1, m2))

	// Wrong ID, but an assigned role.
	m1 = m
	m1.CloudID = "5"
	assert.Equal(t, 9, machineScore(m, m2))

	// Wrong ID, but no Role.
	m1 = m
	m2 = m
	m1.CloudID = "5"
	m1.Role = db.None
	m2.Role = db.None
	assert.Equal(t, 10, machineScore(m1, m2))

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

	// Prefer matching floating IPs over roles. The desired machine is a worker
	// with a floating IP -- the match with a worker with the wrong IP should
	// be worse than a match with a machine with an unknown role, but the same
	// floating IP.
	desired := db.Machine{
		Provider:   db.Amazon,
		Region:     "us-west-1",
		Size:       "m4.large",
		Role:       db.Worker,
		FloatingIP: "desiredFloatingIP",
	}

	masterToBeWithCorrectIP := desired
	masterToBeWithCorrectIP.Role = db.None
	masterToBeWithCorrectIP.FloatingIP = desired.FloatingIP

	workerWithBadIP := desired
	workerWithBadIP.Role = db.Worker
	workerWithBadIP.FloatingIP = "wrong"

	assert.True(t, machineScore(desired, workerWithBadIP) >
		machineScore(desired, masterToBeWithCorrectIP))
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
				From:    []string{"foo"},
				To:      []string{"bar"},
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
				From:    []string{blueprint.PublicInternetLabel},
				To:      []string{"bar"},
				MinPort: 1,
				MaxPort: 2,
			}},
		},
	})
	exp[acl.ACL{CidrIP: "0.0.0.0/0", MinPort: 1, MaxPort: 2}] = struct{}{}
	assert.Equal(t, exp, acls)
}

// Test that syncDBWithBlueprint properly syncs the SSH key information to the database.
func TestJoinSSHKeys(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	adminKey = ""
	expSSHKeys := []string{"exp", "ssh", "keys"}
	cld.conn.Txn(db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {

		bp := view.InsertBlueprint()
		bp.Blueprint.Machines = []blueprint.Machine{{
			Provider: string(FakeAmazon),
			Region:   testRegion,
			Size:     "1",
			SSHKeys:  expSSHKeys,
		}}
		view.Commit(bp)

		m := view.InsertMachine()
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "1"
		m.SSHKeys = []string{"wrong", "ssh", "keys"}
		view.Commit(m)

		cld.syncDBWithBlueprint(view)
		assert.Equal(t, expSSHKeys, view.SelectFromMachine(nil)[0].SSHKeys)

		return nil
	})
}

func scrubID(dbms []db.Machine) (res []db.Machine) {
	for _, dbm := range dbms {
		dbm.ID = 0
		res = append(res, dbm)
	}
	return
}
