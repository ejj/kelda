package supervisor

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/docker"
	"github.com/davecgh/go-spew/spew"
)

func TestNone(t *testing.T) {
	ctx := initTest()

	if len(ctx.fd.running()) > 0 {
		t.Errorf("fd.running = %s; want <empty>", spew.Sdump(ctx.fd.running()))
	}

	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.PrivateIP = "1.2.3.4"
		e.Leader = false
		e.LeaderIP = "5.6.7.8"
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	if len(ctx.fd.running()) > 0 {
		t.Errorf("fd.running = %s; want <none>", spew.Sdump(ctx.fd.running()))
	}

	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}
}

func TestMaster(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	etcdIPs := []string{""}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp := map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	/* Change IP, etcd IPs, and become the leader. */
	ip = "8.8.8.8"
	etcdIPs = []string{"8.8.8.8"}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		e.Leader = true
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:      etcdArgsMaster(ip, etcdIPs),
		Ovsdb:     {"ovsdb-server"},
		Ovnnorthd: {"ovn-northd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	/* Lose leadership. */
	ctx.conn.Transact(func(view db.Database) error {
		e := view.SelectFromEtcd(nil)[0]
		e.Leader = false
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}
}

func TestWorker(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	etcdIPs := []string{ip}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Worker
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp := map[string][]string{
		Etcd:        etcdArgsWorker(etcdIPs),
		Ovsdb:       {"ovsdb-server"},
		Ovsvswitchd: {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	leaderIP := "5.6.7.8"
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Worker
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		e.LeaderIP = leaderIP
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:          etcdArgsWorker(etcdIPs),
		Ovsdb:         {"ovsdb-server"},
		Ovncontroller: {"ovn-controller"},
		Ovsvswitchd:   {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	execExp := [][]string{ovsExecArgs(ip, leaderIP)}
	if !reflect.DeepEqual(ctx.execs, execExp) {
		t.Errorf("execs = %s\n\nwant %s", spew.Sdump(ctx.execs), spew.Sdump(exp))
	}
}

func TestChange(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	leaderIP := "5.6.7.8"
	etcdIPs := []string{ip, leaderIP}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Worker
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		e.LeaderIP = leaderIP
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp := map[string][]string{
		Etcd:          etcdArgsWorker(etcdIPs),
		Ovsdb:         {"ovsdb-server"},
		Ovncontroller: {"ovn-controller"},
		Ovsvswitchd:   {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	execExp := [][]string{ovsExecArgs(ip, leaderIP)}
	if !reflect.DeepEqual(ctx.execs, execExp) {
		t.Errorf("execs = %s\n\nwant %s", spew.Sdump(ctx.execs), spew.Sdump(exp))
	}

	ctx.fd.md.ResetExec()
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		m.Role = db.Master
		view.Commit(m)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		m.Role = db.Worker
		view.Commit(m)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:          etcdArgsWorker(etcdIPs),
		Ovsdb:         {"ovsdb-server"},
		Ovncontroller: {"ovn-controller"},
		Ovsvswitchd:   {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	execExp = [][]string{ovsExecArgs(ip, leaderIP)}
	if !reflect.DeepEqual(ctx.execs, execExp) {
		t.Errorf("execs = %s\n\nwant %s", spew.Sdump(ctx.execs), spew.Sdump(exp))
	}
}

func TestEtcdAdd(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	etcdIPs := []string{ip, "5.6.7.8"}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp := map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	// Add a new master
	etcdIPs = append(etcdIPs, "9.10.11.12")
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
}

func TestEtcdRemove(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	etcdIPs := []string{ip, "5.6.7.8"}
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp := map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	// Remove a master
	etcdIPs = etcdIPs[1:]
	ctx.conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Master
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	ctx.run()

	exp = map[string][]string{
		Etcd:  etcdArgsMaster(ip, etcdIPs),
		Ovsdb: {"ovsdb-server"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
}

type testCtx struct {
	sv    supervisor
	fd    fakeDocker
	execs [][]string

	conn  db.Conn
	trigg db.Trigger
}

func initTest() *testCtx {
	conn := db.New()
	md, dk := docker.NewMock()
	ctx := testCtx{supervisor{}, fakeDocker{dk, md}, nil, conn,
		conn.Trigger(db.MinionTable, db.EtcdTable)}
	ctx.sv.conn = ctx.conn
	ctx.sv.dk = ctx.fd.Client

	ctx.conn.Transact(func(view db.Database) error {
		m := view.InsertMinion()
		m.Self = true
		view.Commit(m)
		e := view.InsertEtcd()
		view.Commit(e)
		return nil
	})
	ctx.sv.runSystemOnce()

	execRun = func(name string, args ...string) error {
		ctx.execs = append(ctx.execs, append([]string{name}, args...))
		return nil
	}

	return &ctx
}

func (ctx *testCtx) run() {
	ctx.execs = nil
	select {
	case <-ctx.trigg.C:
	}
	ctx.sv.runSystemOnce()
}

type fakeDocker struct {
	docker.Client
	md *docker.MockClient
}

func (f fakeDocker) running() map[string][]string {
	containers, _ := f.List(nil)

	res := map[string][]string{}
	for _, c := range containers {
		res[c.Name] = c.Args
	}
	return res
}

func etcdArgsMaster(ip string, etcdIPs []string) []string {
	return []string{
		fmt.Sprintf("--name=master-%s", ip),
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		fmt.Sprintf("--advertise-client-urls=http://%s:2379", ip),
		fmt.Sprintf("--listen-peer-urls=http://%s:2380", ip),
		fmt.Sprintf("--initial-advertise-peer-urls=http://%s:2380", ip),
		"--listen-client-urls=http://0.0.0.0:2379",
		"--heartbeat-interval=500",
		"--initial-cluster-state=new",
		"--election-timeout=5000",
	}
}

func etcdArgsWorker(etcdIPs []string) []string {
	return []string{
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		"--heartbeat-interval=500",
		"--election-timeout=5000",
		"--proxy=on",
	}
}

func ovsExecArgs(ip, leader string) []string {
	return []string{"ovs-vsctl", "set", "Open_vSwitch", ".",
		fmt.Sprintf("external_ids:ovn-remote=\"tcp:%s:6640\"", leader),
		fmt.Sprintf("external_ids:ovn-encap-ip=%s", ip),
		"external_ids:ovn-encap-type=\"geneve\"",
		fmt.Sprintf("external_ids:api_server=\"http://%s:9000\"", leader),
		fmt.Sprintf("external_ids:system-id=\"%s\"", ip),
		"--", "add-br", "quilt-int",
		"--", "set", "bridge", "quilt-int", "fail_mode=secure",
	}
}

func validateImage(image string) {
	switch image {
	case Etcd:
	case Ovnnorthd:
	case Ovncontroller:
	case Ovsvswitchd:
	case Ovsdb:
	default:
		panic("Bad Image")
	}
}

func eq(a1, a2 interface{}) bool {
	return reflect.DeepEqual(a1, a2)
}
