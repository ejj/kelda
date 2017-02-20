package cluster

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/stitch"
	"github.com/stretchr/testify/assert"
)

var FakeAmazon db.Provider = "FakeAmazon"
var FakeVagrant db.Provider = "FakeVagrant"
var amazonCloudConfig = "Amazon Cloud Config"
var vagrantCloudConfig = "Vagrant Cloud Config"
var testRegion = "Fake region"

type providerRequest struct {
	request  machine.Machine
	provider provider
	boot     bool
}

type bootRequest struct {
	size        string
	cloudConfig string
}

type ipRequest struct {
	size        string
	cloudConfig string
	ip          string
}

type fakeProvider struct {
	namespace   string
	machines    map[string]machine.Machine
	idCounter   int
	cloudConfig string

	bootRequests []bootRequest
	stopRequests []string
	updateIPs    []ipRequest
	aclRequests  []acl.ACL
}

func fakeValidRegions(p db.Provider) []string {
	return []string{testRegion}
}

func newFakeProvider(p db.Provider, namespace, region string) (provider, error) {
	var ret fakeProvider
	ret.namespace = namespace
	ret.machines = make(map[string]machine.Machine)
	ret.clearLogs()

	switch p {
	case FakeAmazon:
		ret.cloudConfig = amazonCloudConfig
	case FakeVagrant:
		ret.cloudConfig = vagrantCloudConfig
	default:
		panic("Unreached")
	}

	return &ret, nil
}

func (p *fakeProvider) clearLogs() {
	p.bootRequests = []bootRequest{}
	p.stopRequests = []string{}
	p.aclRequests = []acl.ACL{}
	p.updateIPs = []ipRequest{}
}

func (p *fakeProvider) List() ([]machine.Machine, error) {
	var machines []machine.Machine
	for _, machine := range p.machines {
		machines = append(machines, machine)
	}
	return machines, nil
}

func (p *fakeProvider) Boot(bootSet []machine.Machine) error {
	for _, bootSet := range bootSet {
		p.idCounter++
		idStr := strconv.Itoa(p.idCounter)
		bootSet.ID = idStr
		p.machines[idStr] = bootSet
		p.bootRequests = append(p.bootRequests, bootRequest{size: bootSet.Size,
			cloudConfig: p.cloudConfig})
	}

	return nil
}

func (p *fakeProvider) Stop(machines []machine.Machine) error {
	for _, machine := range machines {
		delete(p.machines, machine.ID)
		p.stopRequests = append(p.stopRequests, machine.ID)
	}
	return nil
}

func (p *fakeProvider) SetACLs(acls []acl.ACL) error {
	p.aclRequests = acls
	return nil
}

func (p *fakeProvider) UpdateFloatingIPs(machines []machine.Machine) error {
	for _, m := range machines {
		p.updateIPs = append(p.updateIPs, ipRequest{
			size:        m.Size,
			cloudConfig: p.cloudConfig,
			ip:          m.FloatingIP,
		})

		p.machines[m.ID] = m
	}

	return nil
}

func (p *fakeProvider) Connect(namespace string) error { return nil }

func (p *fakeProvider) ChooseSize(ram stitch.Range, cpu stitch.Range,
	maxPrice float64) string {
	return ""
}

func newTestCluster(namespace string) *cluster {
	sleep = func(t time.Duration) {}
	mock()
	return newCluster(db.New(), namespace)
}

func TestPanicBadProvider(t *testing.T) {
	temp := allProviders
	defer func() {
		r := recover()
		assert.NotNil(t, r)
		allProviders = temp
	}()
	allProviders = []db.Provider{FakeAmazon}
	conn := db.New()
	newCluster(conn, "test")
}

func TestSyncDB(t *testing.T) {
	checkSyncDB := func(cloudMachines []machine.Machine,
		databaseMachines []db.Machine, expected syncDBResult) {
		dbRes := syncDB(cloudMachines, databaseMachines)

		assert.Equal(t, expected.boot, dbRes.boot, "boot")
		assert.Equal(t, expected.stop, dbRes.stop, "stop")
		assert.Equal(t, expected.updateIPs, dbRes.updateIPs, "updateIPs")
	}

	var noMachines []machine.Machine
	dbNoSize := db.Machine{Provider: FakeAmazon, Region: testRegion}
	cmNoSize := machine.Machine{Provider: FakeAmazon, Region: testRegion}
	dbLarge := db.Machine{Provider: FakeAmazon, Size: "m4.large", Region: testRegion}
	cmLarge := machine.Machine{Provider: FakeAmazon, Size: "m4.large",
		Region: testRegion}

	cmNoIP := machine.Machine{Provider: FakeAmazon}
	cmWithIP := machine.Machine{Provider: FakeAmazon, FloatingIP: "ip"}
	dbNoIP := db.Machine{Provider: FakeAmazon}
	dbWithIP := db.Machine{Provider: FakeAmazon, FloatingIP: "ip"}

	// Test boot with no size
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbNoSize}, syncDBResult{
		boot: []machine.Machine{cmNoSize, cmNoSize},
	})

	// Test boot with size
	checkSyncDB(noMachines, []db.Machine{dbLarge, dbLarge}, syncDBResult{
		boot: []machine.Machine{cmLarge, cmLarge},
	})

	// Test mixed boot
	checkSyncDB(noMachines, []db.Machine{dbNoSize, dbLarge}, syncDBResult{
		boot: []machine.Machine{cmNoSize, cmLarge},
	})

	// Test partial boot
	checkSyncDB([]machine.Machine{cmNoSize}, []db.Machine{dbNoSize, dbLarge},
		syncDBResult{
			boot: []machine.Machine{cmLarge},
		},
	)

	// Test stop
	checkSyncDB([]machine.Machine{cmNoSize, cmNoSize}, []db.Machine{}, syncDBResult{
		stop: []machine.Machine{cmNoSize, cmNoSize},
	})

	// Test partial stop
	checkSyncDB([]machine.Machine{cmNoSize, cmLarge}, []db.Machine{}, syncDBResult{
		stop: []machine.Machine{cmNoSize, cmLarge},
	})

	// Test assign Floating IP
	checkSyncDB([]machine.Machine{cmNoIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []machine.Machine{cmWithIP},
	})

	// Test remove Floating IP
	checkSyncDB([]machine.Machine{cmWithIP}, []db.Machine{dbNoIP}, syncDBResult{
		updateIPs: []machine.Machine{cmNoIP},
	})

	// Test replace Floating IP
	cNewIP := machine.Machine{Provider: FakeAmazon, FloatingIP: "ip^"}
	checkSyncDB([]machine.Machine{cNewIP}, []db.Machine{dbWithIP}, syncDBResult{
		updateIPs: []machine.Machine{cmWithIP},
	})

	// Test bad disk size
	checkSyncDB([]machine.Machine{{DiskSize: 3}}, []db.Machine{{DiskSize: 4}},
		syncDBResult{
			stop: []machine.Machine{{DiskSize: 3}},
			boot: []machine.Machine{{DiskSize: 4}},
		})

}

func TestSync(t *testing.T) {
	type assertion struct {
		boot      []bootRequest
		stop      []string
		updateIPs []ipRequest
	}

	checkSync := func(clst *cluster, provider db.Provider, region string,
		expected assertion) {

		clst.runOnce()
		inst := instance{provider, region}
		providerInst := clst.providers[inst].(*fakeProvider)

		if !emptySlices(expected.boot, providerInst.bootRequests) {
			assert.Equal(t, expected.boot, providerInst.bootRequests,
				"bootRequests")
		}

		if !emptySlices(expected.stop, providerInst.stopRequests) {
			assert.Equal(t, expected.stop, providerInst.stopRequests,
				"stopRequests")
		}

		if !emptySlices(expected.updateIPs, providerInst.updateIPs) {
			assert.Equal(t, expected.updateIPs, providerInst.updateIPs,
				"updateIPs")
		}

		providerInst.clearLogs()
	}

	amazonLargeBoot := bootRequest{size: "m4.large", cloudConfig: amazonCloudConfig}
	amazonXLargeBoot := bootRequest{size: "m4.xlarge",
		cloudConfig: amazonCloudConfig}
	vagrantLargeBoot := bootRequest{size: "vagrant.large",
		cloudConfig: vagrantCloudConfig}

	// Test initial boot
	clst := newTestCluster("ns")
	setNamespace(clst.conn, "ns")
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "m4.large"
		view.Commit(m)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion,
		assertion{boot: []bootRequest{amazonLargeBoot}})

	// Test adding a machine with the same provider
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Region = testRegion
		m.Size = "m4.xlarge"
		view.Commit(m)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion,
		assertion{boot: []bootRequest{amazonXLargeBoot}})

	// Test adding a machine with a different provider
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeVagrant
		m.Region = testRegion
		m.Size = "vagrant.large"
		view.Commit(m)

		return nil
	})
	checkSync(clst, FakeVagrant, testRegion,
		assertion{boot: []bootRequest{vagrantLargeBoot}})

	// Test removing a machine
	var toRemove db.Machine
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toRemove = view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon && m.Size == "m4.xlarge"
		})[0]
		view.Remove(toRemove)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion,
		assertion{stop: []string{toRemove.CloudID}})

	// Test booting a machine with floating IP
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = FakeAmazon
		m.Size = "m4.large"
		m.Region = testRegion
		m.FloatingIP = "ip"
		view.Commit(m)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion, assertion{
		boot: []bootRequest{amazonLargeBoot},
		updateIPs: []ipRequest{{
			size:        "m4.large",
			cloudConfig: amazonCloudConfig,
			ip:          "ip",
		}},
	})

	// Test assigning a floating IP to an existing machine
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toAssign := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon &&
				m.Size == "m4.large" &&
				m.FloatingIP == ""
		})[0]
		toAssign.FloatingIP = "another.ip"
		view.Commit(toAssign)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion, assertion{
		updateIPs: []ipRequest{{
			size:        "m4.large",
			cloudConfig: amazonCloudConfig,
			ip:          "another.ip",
		}},
	})

	// Test removing a floating IP
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		toUpdate := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon &&
				m.Size == "m4.large" &&
				m.FloatingIP == "ip"
		})[0]
		toUpdate.FloatingIP = ""
		view.Commit(toUpdate)

		return nil
	})
	checkSync(clst, FakeAmazon, testRegion, assertion{
		updateIPs: []ipRequest{{
			size:        "m4.large",
			cloudConfig: amazonCloudConfig,
			ip:          "",
		}},
	})

	// Test removing and adding a machine
	clst.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
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
	checkSync(clst, FakeAmazon, testRegion, assertion{
		boot: []bootRequest{amazonXLargeBoot},
		stop: []string{toRemove.CloudID},
	})
}

func TestACLs(t *testing.T) {
	myIP = func() (string, error) {
		return "5.6.7.8", nil
	}

	clst := newTestCluster("ns")
	clst.syncACLs([]string{"admin"},
		[]db.PortRange{
			{
				MinPort: 80,
				MaxPort: 80,
			},
		},
		[]db.Machine{
			{
				Provider: FakeAmazon,
				PublicIP: "8.8.8.8",
				Region:   testRegion,
			},
			{},
		},
	)

	exp := []acl.ACL{
		{
			CidrIP:  "admin",
			MinPort: 1,
			MaxPort: 65535,
		},
		{
			CidrIP:  "5.6.7.8/32",
			MinPort: 1,
			MaxPort: 65535,
		},
		{
			CidrIP:  "0.0.0.0/0",
			MinPort: 80,
			MaxPort: 80,
		},
		{
			CidrIP:  "8.8.8.8/32",
			MinPort: 1,
			MaxPort: 65535,
		},
	}
	inst := instance{FakeAmazon, testRegion}
	actual := clst.providers[inst].(*fakeProvider).aclRequests
	assert.Equal(t, exp, actual)
}

func TestUpdateCluster(t *testing.T) {
	conn := db.New()

	clst := updateCluster(conn, nil)
	assert.Nil(t, clst)

	setNamespace(conn, "ns1")
	clst = updateCluster(conn, clst)
	assert.NotNil(t, clst)
	assert.Equal(t, "ns1", clst.namespace)

	inst := instance{FakeAmazon, testRegion}
	amzn := clst.providers[inst].(*fakeProvider)
	assert.Empty(t, amzn.bootRequests)
	assert.Empty(t, amzn.stopRequests)
	assert.Equal(t, "ns1", amzn.namespace)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Provider = FakeAmazon
		m.Size = "size1"
		m.Region = testRegion
		view.Commit(m)
		return nil
	})

	oldClst := clst
	oldAmzn := amzn

	clst = updateCluster(conn, clst)
	assert.NotNil(t, clst)

	// Pointers shouldn't have changed
	amzn = clst.providers[inst].(*fakeProvider)
	assert.True(t, oldClst == clst)
	assert.True(t, oldAmzn == amzn)

	assert.Empty(t, amzn.stopRequests)
	assert.Equal(t, []bootRequest{{
		size:        "size1",
		cloudConfig: amazonCloudConfig,
	}}, amzn.bootRequests)
	assert.Equal(t, "ns1", amzn.namespace)
	amzn.clearLogs()

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		dbms := view.SelectFromMachine(nil)
		dbms[0].Size = "size2"
		view.Commit(dbms[0])
		return nil
	})

	oldClst = clst
	oldAmzn = amzn
	setNamespace(conn, "ns2")
	clst = updateCluster(conn, clst)
	assert.NotNil(t, clst)

	// Pointers should have changed
	amzn = clst.providers[inst].(*fakeProvider)
	assert.True(t, oldClst != clst)
	assert.True(t, oldAmzn != amzn)

	assert.Equal(t, "ns1", oldAmzn.namespace)
	assert.Empty(t, oldAmzn.bootRequests)
	assert.Empty(t, oldAmzn.stopRequests)

	assert.Equal(t, "ns2", amzn.namespace)
	assert.Equal(t, []bootRequest{{
		size:        "size2",
		cloudConfig: amazonCloudConfig,
	}}, amzn.bootRequests)
	assert.Empty(t, amzn.stopRequests)
}

func TestMultiRegionDeploy(t *testing.T) {
	clst := newTestCluster("ns")
	clst.conn.Txn(db.MachineTable,
		db.ClusterTable).Run(func(view db.Database) error {

		for _, p := range allProviders {
			for _, r := range validRegions(p) {
				m := view.InsertMachine()
				m.Provider = p
				m.Region = r
				m.Size = "size1"
				view.Commit(m)
			}
		}

		c := view.InsertCluster()
		c.Namespace = "ns"
		view.Commit(c)
		return nil
	})

	for i := 0; i < 2; i++ {
		clst.runOnce()
		cloudMachines, err := clst.get()
		assert.NoError(t, err)
		dbMachines := clst.conn.SelectFromMachine(nil)
		joinResult := syncDB(cloudMachines, dbMachines)

		// All machines should be booted
		assert.Empty(t, joinResult.boot)
		assert.Empty(t, joinResult.stop)
		assert.Len(t, joinResult.pairs, len(dbMachines))
	}

	clst.conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		m := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == FakeAmazon &&
				m.Region == validRegions(FakeAmazon)[0]
		})

		assert.Len(t, m, 1)
		view.Remove(m[0])
		return nil
	})

	clst.runOnce()
	machinesRemaining, err := clst.get()
	assert.NoError(t, err)

	assert.NotContains(t, machinesRemaining, machine.Machine{
		Size:     "size1",
		Provider: FakeAmazon,
		Region:   validRegions(FakeAmazon)[0],
	})
	cloudMachines, err := clst.get()
	assert.NoError(t, err)
	dbMachines := clst.conn.SelectFromMachine(nil)
	joinResult := syncDB(cloudMachines, dbMachines)

	assert.Empty(t, joinResult.boot)
	assert.Empty(t, joinResult.stop)
	assert.Len(t, joinResult.pairs, len(dbMachines))
}

func setNamespace(conn db.Conn, ns string) {
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		clst, err := view.GetCluster()
		if err != nil {
			clst = view.InsertCluster()
		}

		clst.Namespace = ns
		view.Commit(clst)
		return nil
	})
}

func mock() {
	newProvider = newFakeProvider
	validRegions = fakeValidRegions
	allProviders = []db.Provider{FakeAmazon, FakeVagrant}
}

func emptySlices(slice1 interface{}, slice2 interface{}) bool {
	return reflect.ValueOf(slice1).Len() == 0 && reflect.ValueOf(slice2).Len() == 0
}
