package cloud

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/db"
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
