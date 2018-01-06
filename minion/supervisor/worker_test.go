package supervisor

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/nl/nlmock"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"
)

func TestWorker(t *testing.T) {
	ctx := initTest()
	ip := "1.2.3.4"
	etcdIPs := []string{ip}
	ctx.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Worker
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	runWorkerOnce()

	exp := map[string][]string{
		images.Etcd:        etcdArgsWorker(etcdIPs),
		images.Ovsdb:       {"ovsdb-server"},
		images.Ovsvswitchd: {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}
	if len(ctx.execs) > 0 {
		t.Errorf("exec = %s; want <empty>", spew.Sdump(ctx.execs))
	}

	leaderIP := "5.6.7.8"
	ctx.conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.MinionSelf()
		e := view.SelectFromEtcd(nil)[0]
		m.Role = db.Worker
		m.PrivateIP = ip
		e.EtcdIPs = etcdIPs
		e.LeaderIP = leaderIP
		view.Commit(m)
		view.Commit(e)
		return nil
	})
	runWorkerOnce()

	exp = map[string][]string{
		images.Etcd:          etcdArgsWorker(etcdIPs),
		images.Ovsdb:         {"ovsdb-server"},
		images.Ovncontroller: {"ovn-controller"},
		images.Ovsvswitchd:   {"ovs-vswitchd"},
	}
	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	execExp := ovsExecArgs(ip, leaderIP)
	if !reflect.DeepEqual(ctx.execs, execExp) {
		t.Errorf("execs = %s\n\nwant %s", spew.Sdump(ctx.execs),
			spew.Sdump(execExp))
	}
}

func TestSetupWorker(t *testing.T) {
	ctx := initTest()

	setupWorker()

	exp := map[string][]string{
		images.Ovsdb:       {"ovsdb-server"},
		images.Ovsvswitchd: {"ovs-vswitchd"},
	}

	if !reflect.DeepEqual(ctx.fd.running(), exp) {
		t.Errorf("fd.running = %s\n\nwant %s", spew.Sdump(ctx.fd.running()),
			spew.Sdump(exp))
	}

	execExp := setupArgs()
	if !reflect.DeepEqual(ctx.execs, execExp) {
		t.Errorf("execs = %s\n\nwant %s", spew.Sdump(ctx.execs),
			spew.Sdump(execExp))
	}
}

func TestCfgGateway(t *testing.T) {
	mk := new(nlmock.I)
	nl.N = mk

	mk.On("LinkByName", "bogus").Return(nil, errors.New("linkByName"))
	ip := net.IPNet{IP: ipdef.GatewayIP, Mask: ipdef.KeldaSubnet.Mask}

	err := cfgGatewayImpl("bogus", ip)
	assert.EqualError(t, err, "no such interface: bogus (linkByName)")

	mk.On("LinkByName", "kelda-int").Return(&netlink.Device{}, nil)
	mk.On("LinkSetUp", mock.Anything).Return(errors.New("linkSetUp"))
	err = cfgGatewayImpl("kelda-int", ip)
	assert.EqualError(t, err, "failed to bring up link: kelda-int (linkSetUp)")

	mk = new(nlmock.I)
	nl.N = mk

	mk.On("LinkByName", "kelda-int").Return(&netlink.Device{}, nil)
	mk.On("LinkSetUp", mock.Anything).Return(nil)
	mk.On("AddrAdd", mock.Anything, mock.Anything).Return(errors.New("addrAdd"))

	err = cfgGatewayImpl("kelda-int", ip)
	assert.EqualError(t, err, "failed to set address: kelda-int (addrAdd)")
	mk.AssertCalled(t, "LinkSetUp", mock.Anything)

	mk = new(nlmock.I)
	nl.N = mk

	mk.On("LinkByName", "kelda-int").Return(&netlink.Device{}, nil)
	mk.On("LinkSetUp", mock.Anything).Return(nil)
	mk.On("AddrAdd", mock.Anything, ip).Return(nil)

	err = cfgGatewayImpl("kelda-int", ip)
	assert.NoError(t, err)
	mk.AssertCalled(t, "LinkSetUp", mock.Anything)
	mk.AssertCalled(t, "AddrAdd", mock.Anything, ip)
}

func setupArgs() [][]string {
	vsctl := []string{
		"ovs-vsctl", "add-br", "kelda-int",
		"--", "set", "bridge", "kelda-int", "fail_mode=secure",
		"other_config:hwaddr=\"02:00:0a:00:00:01\"",
	}
	gateway := []string{"cfgGateway", "10.0.0.1/8"}
	return [][]string{vsctl, gateway}
}

func ovsExecArgs(ip, leader string) [][]string {
	vsctl := []string{"ovs-vsctl", "set", "Open_vSwitch", ".",
		fmt.Sprintf("external_ids:ovn-remote=\"tcp:%s:6640\"", leader),
		fmt.Sprintf("external_ids:ovn-encap-ip=%s", ip),
		"external_ids:ovn-encap-type=\"stt\"",
		fmt.Sprintf("external_ids:api_server=\"http://%s:9000\"", leader),
		fmt.Sprintf("external_ids:system-id=\"%s\"", ip),
	}
	return [][]string{vsctl}
}

func etcdArgsWorker(etcdIPs []string) []string {
	return []string{
		"etcd",
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		"--heartbeat-interval=500",
		"--election-timeout=5000",
		"--proxy=on",
	}
}
