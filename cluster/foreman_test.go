package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/pb"
)

type clients struct {
	clients  map[string]*fakeClient
	newCalls int
}

func TestBoot(t *testing.T) {
	fm, clients := startTest()
	fm.runOnce()

	assert.Zero(t, clients.newCalls)

	fm.conn.Transact(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "1.1.1.1"
		m.PrivateIP = "1.1.1.1."
		m.CloudID = "ID"
		view.Commit(m)
		return nil
	})

	fm.runOnce()
	assert.Equal(t, 1, clients.newCalls)
	_, ok := clients.clients["1.1.1.1"]
	assert.True(t, ok)

	fm.runOnce()
	assert.Equal(t, 1, clients.newCalls)
	_, ok = clients.clients["1.1.1.1"]
	assert.True(t, ok)

	fm.conn.Transact(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "2.2.2.2"
		m.PrivateIP = "2.2.2.2"
		m.CloudID = "ID2"
		view.Commit(m)
		return nil
	})

	fm.runOnce()
	assert.Equal(t, 2, clients.newCalls)

	_, ok = clients.clients["2.2.2.2"]
	assert.True(t, ok)

	_, ok = clients.clients["1.1.1.1"]
	assert.True(t, ok)

	fm.runOnce()
	fm.runOnce()
	fm.runOnce()
	fm.runOnce()
	assert.Equal(t, 2, clients.newCalls)

	_, ok = clients.clients["2.2.2.2"]
	assert.True(t, ok)

	_, ok = clients.clients["1.1.1.1"]
	assert.True(t, ok)

	fm.conn.Transact(func(view db.Database) error {
		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.PublicIP == "1.1.1.1"
		})
		view.Remove(machines[0])
		return nil
	})

	fm.runOnce()
	assert.Equal(t, 2, clients.newCalls)

	_, ok = clients.clients["2.2.2.2"]
	assert.True(t, ok)

	_, ok = clients.clients["1.1.1.1"]
	assert.False(t, ok)

	fm.runOnce()
	fm.runOnce()
	fm.runOnce()
	fm.runOnce()
	assert.Equal(t, 2, clients.newCalls)

	_, ok = clients.clients["2.2.2.2"]
	assert.True(t, ok)

	_, ok = clients.clients["1.1.1.1"]
	assert.False(t, ok)
}

func TestBootEtcd(t *testing.T) {
	fm, clients := startTest()
	fm.conn.Transact(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.PublicIP = "m1-pub"
		m.PrivateIP = "m1-priv"
		m.CloudID = "ignored"
		view.Commit(m)

		m = view.InsertMachine()
		m.Role = db.Worker
		m.PublicIP = "w1-pub"
		m.PrivateIP = "w1-priv"
		m.CloudID = "ignored"
		view.Commit(m)
		return nil
	})
	fm.runOnce()
	assert.Equal(t, []string{"m1-priv"}, clients.clients["w1-pub"].mc.EtcdMembers)

	fm.conn.Transact(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.PublicIP = "m2-pub"
		m.PrivateIP = "m2-priv"
		m.CloudID = "ignored"
		view.Commit(m)
		return nil
	})
	fm.runOnce()
	etcdMembers := clients.clients["w1-pub"].mc.EtcdMembers
	assert.Len(t, etcdMembers, 2)
	assert.Contains(t, etcdMembers, "m1-priv")
	assert.Contains(t, etcdMembers, "m2-priv")

	fm.conn.Transact(func(view db.Database) error {
		var toDelete = view.SelectFromMachine(func(m db.Machine) bool {
			return m.PrivateIP == "m1-priv"
		})[0]
		view.Remove(toDelete)
		return nil
	})
	fm.runOnce()
	assert.Equal(t, []string{"m2-priv"},
		clients.clients["w1-pub"].mc.EtcdMembers)
}

func TestInitForeman(t *testing.T) {
	fm := startTestWithRole(pb.MinionConfig_WORKER)
	fm.conn.Transact(func(view db.Database) error {
		m := view.InsertMachine()
		m.PublicIP = "2.2.2.2"
		m.PrivateIP = "2.2.2.2"
		m.CloudID = "ID2"
		view.Commit(m)
		return nil
	})

	fm.init()
	for _, m := range fm.minions {
		assert.Equal(t, db.Role(db.Worker), m.machine.Role)
	}

	fm = startTestWithRole(pb.MinionConfig_Role(-7))
	for _, m := range fm.minions {
		assert.Equal(t, db.None, m.machine.Role)
	}
}

func TestConfigConsistency(t *testing.T) {
	masterRole := db.RoleToPB(db.Master)
	workerRole := db.RoleToPB(db.Worker)

	fm, _ := startTest()
	var master, worker db.Machine
	fm.conn.Transact(func(view db.Database) error {
		master = view.InsertMachine()
		master.PublicIP = "1.1.1.1"
		master.PrivateIP = master.PublicIP
		master.CloudID = "ID1"
		view.Commit(master)
		worker = view.InsertMachine()
		worker.PublicIP = "2.2.2.2"
		worker.PrivateIP = worker.PublicIP
		worker.CloudID = "ID2"
		view.Commit(worker)
		return nil
	})

	fm.init()
	fm.conn.Transact(func(view db.Database) error {
		master.Role = db.Master
		worker.Role = db.Worker
		view.Commit(master)
		view.Commit(worker)
		return nil
	})

	fm.runOnce()
	checkRoles := func(fore foreman) {
		r := fore.minions["1.1.1.1"].client.(*fakeClient).mc.Role
		assert.Equal(t, masterRole, r)

		r = fore.minions["2.2.2.2"].client.(*fakeClient).mc.Role
		assert.Equal(t, workerRole, r)
	}
	checkRoles(fm)
	fm.stop()

	newfm, clients := startTest()
	newfm.conn = fm.conn

	// Insert the clients into the client list to simulate fetching
	// from the remote cluster
	clients.clients["1.1.1.1"] = &fakeClient{clients, "1.1.1.1",
		pb.MinionConfig{Role: masterRole}}
	clients.clients["2.2.2.2"] = &fakeClient{clients, "2.2.2.2",
		pb.MinionConfig{Role: workerRole}}

	newfm.init()
	newfm.runOnce()
	checkRoles(newfm)

	// After many runs, the roles should never change
	for i := 0; i < 25; i++ {
		newfm.runOnce()
	}
	checkRoles(newfm)

	// Ensure that the DB machines have the correct roles as well.
	newfm.conn.Transact(func(view db.Database) error {
		machines := view.SelectFromMachine(nil)
		for _, m := range machines {
			if m.PublicIP == "1.1.1.1" {
				assert.Equal(t, db.Role(db.Master), m.Role)
			}
			if m.PublicIP == "2.2.2.2" {
				assert.Equal(t, db.Role(db.Worker), m.Role)
			}
		}
		return nil
	})
}

func startTest() (foreman, *clients) {
	fm := createForeman(db.New())
	clients := &clients{make(map[string]*fakeClient), 0}
	fm.newClient = func(ip string) (client, error) {
		if fc, ok := clients.clients[ip]; ok {
			return fc, nil
		}
		fc := &fakeClient{clients, ip, pb.MinionConfig{}}
		clients.clients[ip] = fc
		clients.newCalls++
		return fc, nil
	}
	return fm, clients
}

func startTestWithRole(role pb.MinionConfig_Role) foreman {
	fm := createForeman(db.New())
	clientInst := &clients{make(map[string]*fakeClient), 0}
	fm.newClient = func(ip string) (client, error) {
		fc := &fakeClient{clientInst, ip, pb.MinionConfig{Role: role}}
		clientInst.clients[ip] = fc
		clientInst.newCalls++
		return fc, nil
	}
	return fm
}

type fakeClient struct {
	clients *clients
	ip      string
	mc      pb.MinionConfig
}

func (fc *fakeClient) setMinion(mc pb.MinionConfig) error {
	fc.mc = mc
	return nil
}

func (fc *fakeClient) getMinion() (pb.MinionConfig, error) {
	return fc.mc, nil
}

func (fc *fakeClient) Close() {
	delete(fc.clients.clients, fc.ip)
}
