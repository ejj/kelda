package foreman

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/pb"
)

type clients struct {
	clients  map[string]*fakeClient
	newCalls int
}

func TestBoot(t *testing.T) {
	conn, clients := startTest(t, map[string]pb.MinionConfig_Role{
		"1.1.1.1": pb.MinionConfig_NONE,
		"2.2.2.2": pb.MinionConfig_NONE,
	})
	RunOnce(conn)

	assert.Zero(t, clients.newCalls)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "1.1.1.1"
		m.PrivateIP = "1.1.1.1."
		m.CloudID = "ID"
		view.Commit(m)
		return nil
	})

	RunOnce(conn)
	assert.Equal(t, 1, clients.newCalls)
	assert.Contains(t, clients.clients, "1.1.1.1")

	RunOnce(conn)
	assert.Equal(t, 1, clients.newCalls)
	assert.Contains(t, clients.clients, "1.1.1.1")

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "2.2.2.2"
		m.PrivateIP = "2.2.2.2"
		m.CloudID = "ID2"
		view.Commit(m)
		return nil
	})

	RunOnce(conn)
	assert.Equal(t, 2, clients.newCalls)
	assert.Contains(t, clients.clients, "2.2.2.2")
	assert.Contains(t, clients.clients, "1.1.1.1")

	RunOnce(conn)
	RunOnce(conn)
	RunOnce(conn)
	RunOnce(conn)
	assert.Equal(t, 2, clients.newCalls)
	assert.Contains(t, clients.clients, "2.2.2.2")
	assert.Contains(t, clients.clients, "1.1.1.1")

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.PublicIP == "1.1.1.1"
		})
		view.Remove(machines[0])
		return nil
	})

	RunOnce(conn)
	assert.Equal(t, 2, clients.newCalls)
	assert.Contains(t, clients.clients, "2.2.2.2")
	assert.NotContains(t, clients.clients, "1.1.1.1")

	RunOnce(conn)
	RunOnce(conn)
	RunOnce(conn)
	RunOnce(conn)
	assert.Equal(t, 2, clients.newCalls)
	assert.Contains(t, clients.clients, "2.2.2.2")
	assert.NotContains(t, clients.clients, "1.1.1.1")
}

func TestBootEtcd(t *testing.T) {
	conn, clients := startTest(t, map[string]pb.MinionConfig_Role{
		"m1-pub": pb.MinionConfig_MASTER,
		"m2-pub": pb.MinionConfig_MASTER,
		"w1-pub": pb.MinionConfig_WORKER,
	})

	// Test that the worker connects to the master.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.PublicIP = "m1-pub"
		m.PrivateIP = "m1-priv"
		m.CloudID = "cloud1"
		view.Commit(m)

		m = view.InsertMachine()
		m.Role = db.Worker
		m.PublicIP = "w1-pub"
		m.PrivateIP = "w1-priv"
		m.CloudID = "cloud2"
		view.Commit(m)
		return nil
	})

	RunOnce(conn)
	assert.Equal(t, []string{"m1-priv"}, clients.clients["w1-pub"].mc.EtcdMembers)

	// Test that if we add another master, the worker connects to both masters.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.PublicIP = "m2-pub"
		m.PrivateIP = "m2-priv"
		m.CloudID = "ignored"
		view.Commit(m)
		return nil
	})
	RunOnce(conn)
	etcdMembers := clients.clients["w1-pub"].mc.EtcdMembers
	assert.Len(t, etcdMembers, 2)
	assert.Contains(t, etcdMembers, "m1-priv")
	assert.Contains(t, etcdMembers, "m2-priv")

	// Test that if we remove a master, the worker connects to the remaining master.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		var toDelete = view.SelectFromMachine(func(m db.Machine) bool {
			return m.PrivateIP == "m1-priv"
		})[0]
		view.Remove(toDelete)
		return nil
	})
	RunOnce(conn)
	assert.Equal(t, []string{"m2-priv"},
		clients.clients["w1-pub"].mc.EtcdMembers)
}

func TestBootEtcdRoleConflict(t *testing.T) {
	conn, clients := startTest(t, map[string]pb.MinionConfig_Role{
		"m1-pub": pb.MinionConfig_MASTER,
		"w1-pub": pb.MinionConfig_WORKER,
	})

	// Test that the correct IP is predicted as the master even when the cluster's
	// guess is incorrect.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Worker
		m.PublicIP = "m1-pub"
		m.PrivateIP = "m1-priv"
		m.CloudID = "cloud1"
		view.Commit(m)

		m = view.InsertMachine()
		m.Role = db.Master
		m.PublicIP = "w1-pub"
		m.PrivateIP = "w1-priv"
		m.CloudID = "cloud2"
		view.Commit(m)
		return nil
	})

	RunOnce(conn)
	assert.Equal(t, []string{"m1-priv"}, clients.clients["w1-pub"].mc.EtcdMembers)
}

func TestGetMachineRole(t *testing.T) {
	workerMinion := minion{
		config: pb.MinionConfig{
			Role: pb.MinionConfig_WORKER,
		},
	}
	minions = map[string]*minion{
		"1.1.1.1": &workerMinion,
	}

	assert.Equal(t, db.Role(db.Worker), GetMachineRole("1.1.1.1"))
	assert.Equal(t, db.Role(db.None), GetMachineRole("none"))

	minions = map[string]*minion{}
}

func TestIsConnected(t *testing.T) {
	minions = map[string]*minion{}
	assert.False(t, IsConnected("host"))

	minions["host"] = &minion{connected: false}
	assert.False(t, IsConnected("host"))

	minions["host"].connected = true
	assert.True(t, IsConnected("host"))
}

func startTest(t *testing.T, roles map[string]pb.MinionConfig_Role) (db.Conn, *clients) {
	conn := db.New()
	minions = map[string]*minion{}
	clients := &clients{make(map[string]*fakeClient), 0}
	newClient = func(ip string) (client, error) {
		if fc, ok := clients.clients[ip]; ok {
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
		clients.newCalls++
		return fc, nil
	}
	return conn, clients
}

type fakeClient struct {
	clients *clients
	ip      string
	role    pb.MinionConfig_Role
	mc      pb.MinionConfig

	getMinionError bool
}

func (fc *fakeClient) setMinion(mc pb.MinionConfig) error {
	fc.mc = mc
	return nil
}

func (fc *fakeClient) getMinion() (pb.MinionConfig, error) {
	if fc.getMinionError {
		return pb.MinionConfig{}, errors.New("mock error")
	}

	mc := fc.mc
	mc.Role = fc.role
	return mc, nil
}

func (fc *fakeClient) Close() {
	delete(fc.clients.clients, fc.ip)
}
