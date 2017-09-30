package cloud

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/stretchr/testify/assert"
)

var FakeAmazon db.ProviderName = "FakeAmazon"
var FakeVagrant db.ProviderName = "FakeVagrant"
var testRegion = "Fake region"

type fakeProvider struct {
	providerName db.ProviderName
	region       string
	namespace    string
	machines     map[string]db.Machine
	roles        map[string]db.Role
	idCounter    int
	cloudConfig  string

	bootRequests []db.Machine
	stopRequests []string
	updatedIPs   []db.Machine
	aclRequests  []acl.ACL

	listError error
}

func fakeValidRegions(p db.ProviderName) []string {
	return []string{testRegion}
}

func (p *fakeProvider) clearLogs() {
	p.bootRequests = nil
	p.stopRequests = nil
	p.aclRequests = nil
	p.updatedIPs = nil
}

func (p *fakeProvider) List() ([]db.Machine, error) {
	if p.listError != nil {
		return nil, p.listError
	}

	var machines []db.Machine
	for _, machine := range p.machines {
		machines = append(machines, machine)
	}
	return machines, nil
}

func (p *fakeProvider) Boot(bootSet []db.Machine) error {
	for _, toBoot := range bootSet {
		// Record the boot request before we mutate it with implementation
		// details of our fakeProvider.
		p.bootRequests = append(p.bootRequests, toBoot)

		p.idCounter++
		idStr := strconv.Itoa(p.idCounter)
		toBoot.CloudID = idStr
		toBoot.PublicIP = idStr

		// A machine's role is `None` until the minion boots, at which
		// `getMachineRoles` will populate this field with the correct role.
		// We simulate this by setting the role of the machine returned by
		// `List()` to be None, and only return the correct role in
		// `getMachineRole`.
		p.roles[toBoot.PublicIP] = toBoot.Role
		toBoot.Role = db.None

		p.machines[idStr] = toBoot
	}

	return nil
}

func (p *fakeProvider) Stop(machines []db.Machine) error {
	for _, machine := range machines {
		delete(p.machines, machine.CloudID)
		p.stopRequests = append(p.stopRequests, machine.CloudID)
	}
	return nil
}

func (p *fakeProvider) SetACLs(acls []acl.ACL) error {
	p.aclRequests = acls
	return nil
}

func (p *fakeProvider) UpdateFloatingIPs(machines []db.Machine) error {
	for _, desired := range machines {
		curr := p.machines[desired.CloudID]
		curr.FloatingIP = desired.FloatingIP
		p.machines[desired.CloudID] = curr
	}
	p.updatedIPs = append(p.updatedIPs, machines...)
	return nil
}

func newTestCloud(provider db.ProviderName, region, namespace string) *cloud {
	sleep = func(t time.Duration) {}
	mock()
	cld, _ := newCloud(db.New(), provider, region, namespace)
	return &cld
}

func TestPanicBadProvider(t *testing.T) {
	temp := db.AllProviders
	defer func() {
		r := recover()
		assert.NotNil(t, r)
		db.AllProviders = temp
	}()
	db.AllProviders = []db.ProviderName{FakeAmazon}
	conn := db.New()
	newCloud(conn, FakeAmazon, testRegion, "test")
}

func TestSyncDB(t *testing.T) {
	checkSyncDB := func(cloudMachines []db.Machine,
		databaseMachines []db.Machine, expected syncDBResult) syncDBResult {
		dbRes := syncDB(cloudMachines, databaseMachines)

		assert.Equal(t, expected.boot, dbRes.boot, "boot")
		assert.Equal(t, expected.stop, dbRes.stop, "stop")
		assert.Equal(t, expected.updateIPs, dbRes.updateIPs, "updateIPs")

		return dbRes
	}

	var noMachines []db.Machine
	dbNoSize := db.Machine{Provider: FakeAmazon, Region: testRegion}
	cmNoSize := db.Machine{Provider: FakeAmazon, Region: testRegion}
	dbLarge := db.Machine{Provider: FakeAmazon, Size: "m4.large", Region: testRegion}
	cmLarge := db.Machine{
		Provider: FakeAmazon,
		Region:   testRegion,
		Size:     "m4.large",
	}

	dbMaster := db.Machine{Provider: FakeAmazon, Role: db.Master}
	cmMasterList := db.Machine{Provider: FakeAmazon, Role: db.Master}
	dbWorker := db.Machine{Provider: FakeAmazon, Role: db.Worker}
	cmWorkerList := db.Machine{Provider: FakeAmazon, Role: db.Worker}

	cmNoIP := db.Machine{Provider: FakeAmazon, CloudID: "id"}
	cmWithIP := db.Machine{
		Provider:   FakeAmazon,
		CloudID:    "id",
		FloatingIP: "ip",
	}
	dbNoIP := db.Machine{Provider: FakeAmazon, CloudID: "id"}
	dbWithIP := db.Machine{Provider: FakeAmazon, CloudID: "id", FloatingIP: "ip"}

	// Test boot with no size
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbNoSize}, syncDBResult{
		boot: []db.Machine{dbNoSize, dbNoSize},
	})

	// Test boot with size
	checkSyncDB(noMachines, []db.Machine{dbLarge, dbLarge}, syncDBResult{
		boot: []db.Machine{dbLarge, dbLarge},
	})

	// Test mixed boot
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbLarge}, syncDBResult{
		boot: []db.Machine{dbNoSize, dbLarge},
	})

	// Test partial boot
	checkSyncDB([]db.Machine{cmNoSize}, []db.Machine{dbNoSize, dbLarge},
		syncDBResult{
			boot: []db.Machine{dbLarge},
		},
	)

	// Test stop
	checkSyncDB([]db.Machine{cmNoSize, cmNoSize}, []db.Machine{}, syncDBResult{
		stop: []db.Machine{cmNoSize, cmNoSize},
	})

	// Test partial stop
	checkSyncDB([]db.Machine{cmNoSize, cmLarge}, []db.Machine{}, syncDBResult{
		stop: []db.Machine{cmNoSize, cmLarge},
	})

	// Test assign Floating IP
	checkSyncDB([]db.Machine{cmNoIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []db.Machine{cmWithIP},
	})

	// Test remove Floating IP
	checkSyncDB([]db.Machine{cmWithIP}, []db.Machine{dbNoIP}, syncDBResult{
		updateIPs: []db.Machine{cmNoIP},
	})

	// Test replace Floating IP
	cNewIP := db.Machine{
		Provider:   FakeAmazon,
		CloudID:    "id",
		FloatingIP: "ip^",
	}
	checkSyncDB([]db.Machine{cNewIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []db.Machine{cmWithIP},
	})

	// Test bad disk size
	checkSyncDB([]db.Machine{{DiskSize: 3}},
		[]db.Machine{{DiskSize: 4}},
		syncDBResult{
			stop: []db.Machine{{DiskSize: 3}},
			boot: []db.Machine{{DiskSize: 4}},
		})

	// Test different roles
	checkSyncDB([]db.Machine{cmWorkerList}, []db.Machine{dbMaster}, syncDBResult{
		boot: []db.Machine{dbMaster},
		stop: []db.Machine{cmWorkerList},
	})

	checkSyncDB([]db.Machine{cmMasterList}, []db.Machine{dbWorker}, syncDBResult{
		boot: []db.Machine{dbWorker},
		stop: []db.Machine{cmMasterList},
	})

	// Test reserved instances.
	checkSyncDB([]db.Machine{{Preemptible: true}},
		[]db.Machine{{Preemptible: false}},
		syncDBResult{
			boot: []db.Machine{{Preemptible: false}},
			stop: []db.Machine{{Preemptible: true}},
		})

	// Test matching role as priority over PublicIP
	dbMaster.PublicIP = "worker"
	cmMasterList.PublicIP = "master"
	dbWorker.PublicIP = "master"
	cmWorkerList.PublicIP = "worker"

	checkSyncDB([]db.Machine{cmMasterList, cmWorkerList},
		[]db.Machine{dbMaster, dbWorker},
		syncDBResult{})

	// Test shuffling roles before CloudID is assigned
	dbw1 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w1"}
	dbw2 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w2"}
	dbw3 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "w3"}

	mw1 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw1", PublicIP: "w1"}
	mw2 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw2", PublicIP: "w2"}
	mw3 := db.Machine{Provider: FakeAmazon, Role: db.Worker,
		CloudID: "mw3", PublicIP: "w3"}

	pair1 := join.Pair{L: dbw1, R: mw1}
	pair2 := join.Pair{L: dbw2, R: mw2}
	pair3 := join.Pair{L: dbw3, R: mw3}

	exp := []join.Pair{
		pair1,
		pair2,
		pair3,
	}

	pairs := checkSyncDB([]db.Machine{mw1, mw2, mw3},
		[]db.Machine{dbw1, dbw2, dbw3},
		syncDBResult{})

	assert.Equal(t, exp, pairs.pairs)

	// Test FloatingIP without role
	dbf1 := db.Machine{Provider: FakeAmazon, Role: db.Master, PublicIP: "master"}
	dbf2 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "worker",
		FloatingIP: "float"}

	cmf1 := db.Machine{Provider: FakeAmazon, PublicIP: "worker", CloudID: "worker"}
	cmf2 := db.Machine{Provider: FakeAmazon, PublicIP: "master", CloudID: "master"}

	// No roles, CloudIDs not assigned, so nothing should happen
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{})

	cmf1.Role = db.Worker

	// One role assigned, so one CloudID to be assigned after
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{})

	dbf2.CloudID = cmf1.CloudID
	cmf2.Role = db.Master

	// Now that CloudID of machine with FloatingIP has been assigned,
	// FloatingIP should also be assigned
	checkSyncDB([]db.Machine{cmf1, cmf2},
		[]db.Machine{dbf1, dbf2},
		syncDBResult{
			updateIPs: []db.Machine{
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "worker",
					CloudID:    "worker",
					FloatingIP: "float",
				},
			},
		})

	// Test FloatingIP role shuffling
	dbm2 := db.Machine{Provider: FakeAmazon, Role: db.Master, PublicIP: "mIP"}
	dbm3 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "wIP1",
		FloatingIP: "flip1"}
	dbm4 := db.Machine{Provider: FakeAmazon, Role: db.Worker, PublicIP: "wIP2",
		FloatingIP: "flip2"}

	m2 := db.Machine{Provider: FakeAmazon, PublicIP: "mIP", CloudID: "m2"}
	m3 := db.Machine{Provider: FakeAmazon, PublicIP: "wIP1", CloudID: "m3"}
	m4 := db.Machine{Provider: FakeAmazon, PublicIP: "wIP2", CloudID: "m4"}

	m2.Role = db.Worker
	m3.Role = db.Master
	m4.Role = db.Worker

	// CloudIDs not assigned to db machines yet, so shouldn't update anything.
	checkSyncDB([]db.Machine{m2, m3, m4},
		[]db.Machine{dbm2, dbm3, dbm4},
		syncDBResult{})

	dbm2.CloudID = m3.CloudID
	dbm3.CloudID = m2.CloudID
	dbm4.CloudID = m4.CloudID

	// CloudIDs are now assigned, so time to update floating IPs
	checkSyncDB([]db.Machine{m2, m3, m4},
		[]db.Machine{dbm2, dbm3, dbm4},
		syncDBResult{
			updateIPs: []db.Machine{
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "mIP",
					CloudID:    "m2",
					FloatingIP: "flip1",
				},
				{
					Provider:   FakeAmazon,
					Role:       db.Worker,
					PublicIP:   "wIP2",
					CloudID:    "m4",
					FloatingIP: "flip2",
				},
			},
		})

}

func TestCloudRunOnce(t *testing.T) {
	type ipRequest struct {
		id string
		ip string
	}

	type assertion struct {
		boot      []db.Machine
		stop      []string
		updateIPs []ipRequest
	}

	checkSync := func(cld *cloud, expected assertion) {
		cld.runOnce()
		providerInst := cld.provider.(*fakeProvider)

		assert.Equal(t, expected.boot, providerInst.bootRequests, "bootRequests")

		assert.Equal(t, expected.stop, providerInst.stopRequests, "stopRequests")

		var updatedIPs []ipRequest
		for _, m := range providerInst.updatedIPs {
			updatedIPs = append(updatedIPs,
				ipRequest{id: m.CloudID, ip: m.FloatingIP})
		}
		assert.Equal(t, expected.updateIPs, updatedIPs, "updateIPs")

		providerInst.clearLogs()
	}

	// Test initial boot
	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	setNamespace(cld.conn, "ns")
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "m4.large"
		view.Commit(m)

		return nil
	})
	checkSync(cld, assertion{boot: []db.Machine{{
		Provider: FakeAmazon,
		Region:   testRegion,
		Size:     "m4.large",
		Role:     db.Master},
	}})

	// Test adding a machine with the same provider
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "m4.xlarge"
		view.Commit(m)

		return nil
	})
	checkSync(cld, assertion{boot: []db.Machine{{
		Provider: FakeAmazon,
		Region:   testRegion,
		Size:     "m4.xlarge",
		Role:     db.Master},
	}})

	// Test removing a machine
	var toRemove db.Machine
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toRemove = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon && m.Size == "m4.xlarge"
		})[0]
		view.Remove(toRemove)

		return nil
	})
	checkSync(cld, assertion{stop: []string{toRemove.CloudID}})

	// Test booting a machine with floating IP - shouldn't update FloatingIP yet
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Size = "m4.large"
		m.Region = testRegion
		m.FloatingIP = "ip"
		view.Commit(m)

		return nil
	})
	checkSync(cld, assertion{
		boot: []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "m4.large",
			Role:     db.Master}},
	})

	// The bootRequest from the previous test is done now, and a CloudID has
	// been assigned, so we should also receive the ipRequest from before
	checkSync(cld, assertion{updateIPs: []ipRequest{{id: "3", ip: "ip"}}})

	// Test assigning a floating IP to an existing machine
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toAssign := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon &&
				m.Size == "m4.large" &&
				m.FloatingIP == ""
		})[0]
		toAssign.FloatingIP = "another.ip"
		view.Commit(toAssign)

		return nil
	})
	checkSync(cld, assertion{updateIPs: []ipRequest{{id: "1", ip: "another.ip"}}})

	// Test removing a floating IP
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toUpdate := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon &&
				m.Size == "m4.large" &&
				m.FloatingIP == "ip"
		})[0]
		toUpdate.FloatingIP = ""
		view.Commit(toUpdate)

		return nil
	})
	checkSync(cld, assertion{updateIPs: []ipRequest{{id: "3", ip: ""}}})

	// Test removing and adding a machine
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toRemove = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon && m.Size == "m4.large"
		})[0]
		view.Remove(toRemove)

		m := view.InsertMachine()
		m.Role = db.Worker
		m.Provider = FakeAmazon
		m.Size = "m4.xlarge"
		m.Region = testRegion
		view.Commit(m)

		return nil
	})
	checkSync(cld, assertion{
		boot: []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "m4.xlarge",
			Role:     db.Worker}},
		stop: []string{toRemove.CloudID},
	})

	// Test adding machine with different role
	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Size = "m4.xlarge"
		m.Region = testRegion
		view.Commit(m)

		return nil
	})

	checkSync(cld, assertion{
		boot: []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "m4.xlarge",
			Role:     db.Master}},
	})

	cld.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toRemove = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Role == db.Master && m.Size == "m4.xlarge" &&
				m.Provider == FakeAmazon
		})[0]
		view.Remove(toRemove)
		m := view.InsertMachine()
		m.Role = db.Worker
		m.Provider = FakeAmazon
		m.Size = "m4.xlarge"
		m.Region = testRegion
		view.Commit(m)

		return nil
	})

	checkSync(cld, assertion{
		boot: []db.Machine{{
			Provider: FakeAmazon,
			Region:   testRegion,
			Size:     "m4.xlarge",
			Role:     db.Worker}},
		stop: []string{toRemove.CloudID},
	})
}

func TestACLs(t *testing.T) {
	myIP = func() (string, error) {
		return "5.6.7.8", nil
	}

	clst := newTestCloud(FakeAmazon, testRegion, "ns")
	clst.syncACLs([]acl.ACL{{CidrIP: "local", MinPort: 80, MaxPort: 80}})

	exp := []acl.ACL{
		{
			CidrIP:  "5.6.7.8/32",
			MinPort: 80,
			MaxPort: 80,
		},
	}
	actual := clst.provider.(*fakeProvider).aclRequests
	assert.Equal(t, exp, actual)
}

func TestGetACLs(t *testing.T) {
	cld := newTestCloud(FakeAmazon, testRegion, "ns")

	exp := map[acl.ACL]struct{}{
		{CidrIP: "local", MinPort: 1, MaxPort: 65535}: {},
	}

	// Empty blueprint should have "local" added to it.
	acls := cld.getACLs(db.Blueprint{})
	assert.Equal(t, exp, acls)

	// A blueprint with local, shouldn't have it added a second time.
	acls = cld.getACLs(db.Blueprint{
		Blueprint: blueprint.Blueprint{AdminACL: []string{"local"}},
	})
	assert.Equal(t, exp, acls)

	// Connections that aren't to or from public, shouldn't affect the acls.
	acls = cld.getACLs(db.Blueprint{
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
	acls = cld.getACLs(db.Blueprint{
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

func TestMakeClouds(t *testing.T) {
	stop := make(chan struct{})
	makeClouds(db.New(), "ns", stop)

	var locations []string
	for _, p := range instantiatedProviders {
		loc := fmt.Sprintf("%s-%s-%s", p.providerName, p.region, p.namespace)
		locations = append(locations, loc)
	}

	// Verify that each cloud provider gets instantiated.
	assert.Equal(t, []string{
		"FakeAmazon-Fake region-ns",
		"FakeAmazon-Fake region-ns",
		"FakeVagrant-Fake region-ns"}, locations)
	close(stop)
}

func TestGetError(t *testing.T) {
	t.Parallel()

	cld := newTestCloud(FakeAmazon, testRegion, "ns")
	cld.provider.(*fakeProvider).listError = errors.New("err")
	_, err := cld.get()
	assert.EqualError(t, err, "list FakeAmazon-Fake region-ns: err")
}

func TestNewProviderFailure(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("provider.New did not panic on invalid provider")
		}
	}()
	newProviderImpl("FakeAmazon", testRegion, "namespace")
}

func setNamespace(conn db.Conn, ns string) {
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		bp, err := view.GetBlueprint()
		if err != nil {
			bp = view.InsertBlueprint()
		}

		bp.Namespace = ns
		view.Commit(bp)
		return nil
	})
}

var instantiatedProviders []fakeProvider

func mock() {
	instantiatedProviders = nil
	newProvider = func(p db.ProviderName, namespace,
		region string) (provider, error) {
		ret := fakeProvider{
			providerName: p,
			region:       region,
			namespace:    namespace,
			machines:     make(map[string]db.Machine),
			roles:        make(map[string]db.Role),
		}
		ret.clearLogs()

		instantiatedProviders = append(instantiatedProviders, ret)
		return &ret, nil
	}

	validRegions = fakeValidRegions
	db.AllProviders = []db.ProviderName{FakeAmazon, FakeVagrant}
	getMachineRole = func(ip string) db.Role {
		for _, prvdr := range instantiatedProviders {
			if role, ok := prvdr.roles[ip]; ok {
				return role
			}
		}
		return db.None
	}
}
