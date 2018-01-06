package foreman

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/pb"
)

type clients struct {
	clients        map[string]*fakeClient
	newClientError bool
	getMinionError bool
}

func TestUpdateMinions(t *testing.T) {
	newMinionCalls := 0
	newMinion = func(conn db.Conn, cloudID string, stop chan struct{}) {
		newMinionCalls++
	}
	conn := db.New()

	minionChans := make(map[string]chan struct{})
	machines := []db.Machine{
		{PublicIP: "1.1.1.1", CloudID: "ID1"},
		{PublicIP: "2.2.2.2", CloudID: "ID2"},
	}

	updateMinions(conn, machines, minionChans)
	// Give the minions time to be created.
	for i := 0; i < 20 && newMinionCalls < 2; i++ {
		time.Sleep(500 * time.Millisecond)
	}

	assert.Equal(t, 2, newMinionCalls)
	assert.Contains(t, minionChans, "ID1")
	assert.Contains(t, minionChans, "ID2")

	// Removed machine.
	machines = []db.Machine{{PublicIP: "2.2.2.2", CloudID: "ID2"}}
	expectStop := minionChans["ID1"]
	updateMinions(conn, machines, minionChans)
	assert.Equal(t, 2, newMinionCalls)
	assert.NotContains(t, minionChans, "ID1")
	assert.Contains(t, minionChans, "ID2")
	_, more := <-expectStop
	assert.False(t, more)

	// Create a new thread when a machine is replaced by a machine with the same IP.
	machines = []db.Machine{{PublicIP: "2.2.2.2", CloudID: "ID22"}}
	expectStop = minionChans["ID2"]
	updateMinions(conn, machines, minionChans)

	for i := 0; i < 20 && newMinionCalls < 3; i++ {
		time.Sleep(500 * time.Millisecond)
	}
	assert.Equal(t, 3, newMinionCalls)
	assert.NotContains(t, minionChans, "ID2")
	assert.Contains(t, minionChans, "ID22")
	_, more = <-expectStop
	assert.False(t, more)

	machines = []db.Machine{}
	expectStop = minionChans["ID22"]
	updateMinions(conn, machines, minionChans)
	assert.Equal(t, 3, newMinionCalls)
	assert.Len(t, minionChans, 0)
	_, more = <-expectStop
	assert.False(t, more)

	machines = []db.Machine{{PublicIP: "3.3.3.3", CloudID: "ID3"}}
	updateMinions(conn, machines, minionChans)

	for i := 0; i < 20 && newMinionCalls < 4; i++ {
		time.Sleep(500 * time.Millisecond)
	}
	assert.Len(t, minionChans, 1)
	assert.Contains(t, minionChans, "ID3")
	assert.Equal(t, 4, newMinionCalls)
}

func TestMakeConfig(t *testing.T) {
	machine1 := db.Machine{
		PublicIP:  "1.1.1.1",
		Role:      db.Worker,
		PrivateIP: "10.10.10.10",
		CloudID:   "ID1",
	}

	machine2 := db.Machine{
		PublicIP:  "2.2.2.2",
		Role:      db.Master,
		PrivateIP: "20.20.20.20",
		CloudID:   "ID2",
	}
	allMachines := []db.Machine{machine1, machine2}

	config := makeConfig(allMachines, machine1, `{"Namespace":"ns"}`)
	assert.Equal(t, "10.10.10.10", config.PrivateIP)
	assert.Equal(t, `{"Namespace":"ns"}`, config.Blueprint)
	assert.Len(t, config.EtcdMembers, 1)
	assert.Contains(t, config.EtcdMembers, "20.20.20.20")

	config = makeConfig(allMachines, machine2, `{"Namespace":"ns"}`)
	assert.Equal(t, "20.20.20.20", config.PrivateIP)
	assert.Equal(t, `{"Namespace":"ns"}`, config.Blueprint)
	assert.Len(t, config.EtcdMembers, 1)
	assert.Contains(t, config.EtcdMembers, "20.20.20.20")

	machine3 := db.Machine{
		PublicIP:  "3.3.3.3",
		Role:      db.Master,
		PrivateIP: "30.30.30.30",
		CloudID:   "ID3",
	}

	allMachines = append(allMachines, machine3)

	config = makeConfig(allMachines, machine1, `{"Namespace":"ns"}`)
	assert.Equal(t, "10.10.10.10", config.PrivateIP)
	assert.Equal(t, `{"Namespace":"ns"}`, config.Blueprint)
	assert.Len(t, config.EtcdMembers, 2)
	assert.Contains(t, config.EtcdMembers, "20.20.20.20")
	assert.Contains(t, config.EtcdMembers, "30.30.30.30")

	allMachines = []db.Machine{machine1, machine3}

	config = makeConfig(allMachines, machine1, `{"Namespace":"ns"}`)
	assert.Equal(t, "10.10.10.10", config.PrivateIP)
	assert.Equal(t, `{"Namespace":"ns"}`, config.Blueprint)
	assert.Len(t, config.EtcdMembers, 1)
	assert.Contains(t, config.EtcdMembers, "30.30.30.30")
}

func TestClusterReady(t *testing.T) {
	t.Parallel()

	readyMachine := db.Machine{
		PrivateIP: "ip",
		Role:      db.Master,
	}

	missingIP := readyMachine
	missingIP.PrivateIP = ""
	assert.False(t, clusterReady([]db.Machine{readyMachine, missingIP}))

	missingRole := readyMachine
	missingRole.Role = db.None
	assert.False(t, clusterReady([]db.Machine{readyMachine, missingRole}))

	assert.True(t, clusterReady([]db.Machine{readyMachine}))
}

func TestForemanRunOnce(t *testing.T) {
	conn := db.New()
	clients := mock(t, map[string]pb.MinionConfig_Role{
		"1.1.1.1": pb.MinionConfig_WORKER,
	})

	config, connected := runOnce(time.Time{}, conn, "ID1")
	assert.False(t, connected)
	assert.Equal(t, db.Role(db.None), db.PBToRole(config.Role))

	conn.Txn(db.MachineTable, db.BlueprintTable).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "1.1.1.1"
		m.Role = db.Worker
		m.Size = "size1"
		m.PrivateIP = "10.10.10.10"
		m.CloudID = "ID1"
		view.Commit(m)
		return nil
	})

	clients.newClientError = true
	config, connected = runOnce(time.Time{}, conn, "ID1")
	assert.False(t, connected)
	assert.Equal(t, db.Role(db.None), db.PBToRole(config.Role))

	clients.newClientError = false
	config, connected = runOnce(time.Time{}, conn, "ID1")
	assert.True(t, connected)
	assert.Equal(t, db.Role(db.Worker), db.PBToRole(config.Role))

	minionConf := clients.clients["1.1.1.1"].mc
	assert.Equal(t, "10.10.10.10", minionConf.PrivateIP)
	assert.Equal(t, "size1", minionConf.Size)

	clients.getMinionError = true
	config, connected = runOnce(time.Time{}, conn, "ID1")
	assert.False(t, connected)
	assert.Equal(t, db.Role(db.None), db.PBToRole(config.Role))
}

func TestGetMachineRole(t *testing.T) {
	setMinionStatus("ID1", pb.MinionConfig{Role: pb.MinionConfig_WORKER}, false)

	assert.Equal(t, db.Role(db.Worker), GetMachineRole("ID1"))
	assert.Equal(t, db.Role(db.None), GetMachineRole("none"))
}

func TestIsConnected(t *testing.T) {
	assert.False(t, IsConnected("host"))

	setMinionStatus("host", pb.MinionConfig{Role: pb.MinionConfig_WORKER}, false)
	assert.False(t, IsConnected("host"))

	setMinionStatus("host", pb.MinionConfig{Role: pb.MinionConfig_WORKER}, true)
	assert.True(t, IsConnected("host"))
}

func mock(t *testing.T, roles map[string]pb.MinionConfig_Role) *clients {
	clients := &clients{make(map[string]*fakeClient), false, false}
	newClient = func(ip string) (client, error) {
		if clients.newClientError {
			return nil, errors.New("newMinion error")
		}

		if fc, ok := clients.clients[ip]; ok && !fc.closed {
			return fc, nil
		}

		role, ok := roles[ip]
		if !ok {
			t.Errorf("no role specified for %s", ip)
		}
		fc := &fakeClient{
			clients: clients,
			ip:      ip,
			role:    role,
		}
		clients.clients[ip] = fc
		return fc, nil
	}
	return clients
}

type fakeClient struct {
	clients *clients
	ip      string
	role    pb.MinionConfig_Role
	mc      pb.MinionConfig
	closed  bool
}

func (fc *fakeClient) setMinion(mc pb.MinionConfig) error {
	fc.mc = mc
	return nil
}

func (fc *fakeClient) getMinion() (pb.MinionConfig, error) {
	if fc.clients.getMinionError {
		return pb.MinionConfig{}, errors.New("mock error")
	}

	mc := fc.mc
	mc.Role = fc.role
	return mc, nil
}

func (fc *fakeClient) Close() {
	fc.clients.clients[fc.ip].closed = true
}
