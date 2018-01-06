package supervisor

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/nl/nlmock"
	"github.com/kelda/kelda/util/str"
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
		EtcdName:        etcdArgsWorker(etcdIPs),
		OvsdbName:       {"ovsdb-server"},
		OvsvswitchdName: {"ovs-vswitchd"},
	}
	assert.Equal(t, exp, ctx.fd.running())
	assert.Empty(t, ctx.execs)

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
		EtcdName:          etcdArgsWorker(etcdIPs),
		OvsdbName:         {"ovsdb-server"},
		OvncontrollerName: {"ovn-controller"},
		OvsvswitchdName:   {"ovs-vswitchd"},
		KubeletName:       kubeletArgs(ip),
	}
	assert.Equal(t, exp, ctx.fd.running())

	assert.Equal(t, [][]string{
		{"cfgOvn", "1.2.3.4", "5.6.7.8"},
	}, ctx.execs)
}

func TestSetupWorker(t *testing.T) {
	ctx := initTest()

	setupWorker()

	exp := map[string][]string{
		OvsdbName:       {"ovsdb-server"},
		OvsvswitchdName: {"ovs-vswitchd"},
	}
	assert.Equal(t, exp, ctx.fd.running())
	assert.Equal(t, setupArgs(), ctx.execs)
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

func TestCfgOVN(t *testing.T) {
	type mockSetCall struct {
		args   []string
		called bool
	}
	expGetArgs := []string{"--if-exists", "get", "Open_vSwitch", ".",
		"external_ids:ovn-remote",
		"external_ids:ovn-encap-ip",
		"external_ids:ovn-encap-type",
		"external_ids:api_server",
		"external_ids:system-id"}

	// setupExec sets up the mock environment for execRun. It configures
	// execRun such that if the configuration get command is called, the
	// getResp is returned.  It also returns a pointer to *mockSetCall, whose
	// contents will be updated if the configuration set command is called.
	setupExec := func(getResp string) *mockSetCall {
		var result mockSetCall
		execRun = func(name string, args ...string) ([]byte, error) {
			switch {
			case name == "ovs-vsctl" && reflect.DeepEqual(args, expGetArgs):
				return []byte(getResp), nil
			case name == "ovs-vsctl" && str.SliceContains(args, "set"):
				result.called = true
				result.args = args
				return []byte("ignored"), nil
			default:
				t.Errorf("Unexpected exec call: %v",
					append([]string{name}, args...))
				return nil, errors.New("unreached")
			}
		}
		return &result
	}

	myIP := "ip"
	leaderIP := "leader"

	// Test that if the values have not yet been set, set is called.
	result := setupExec("\n\n\n\n\n")
	assert.NoError(t, cfgOVNImpl(myIP, leaderIP))
	assert.True(t, result.called)
	assert.Equal(t, ovsExecSetArgs(myIP, leaderIP), result.args)

	// Test that if the values are already correct, set is not called.
	getResp := fmt.Sprintf(`"tcp:%[2]s:6640"
%[1]q
stt
"http://%[2]s:9000"
%[1]q
`, myIP, leaderIP)
	result = setupExec(getResp)
	assert.NoError(t, cfgOVNImpl(myIP, leaderIP))
	assert.False(t, result.called)

	// Test that if the leader IP changes, set is called with the new IP.
	leaderIP = "leader2"
	result = setupExec(getResp)
	assert.NoError(t, cfgOVNImpl(myIP, leaderIP))
	assert.True(t, result.called)
	assert.Equal(t, ovsExecSetArgs(myIP, leaderIP), result.args)
}

func TestCfgOVNErrors(t *testing.T) {
	setupExec := func(getShouldError, setShouldError bool) {
		execRun = func(name string, args ...string) ([]byte, error) {
			switch {
			case str.SliceContains(args, "get"):
				if getShouldError {
					return nil, assert.AnError
				}
				return nil, nil
			case str.SliceContains(args, "set"):
				if setShouldError {
					return nil, assert.AnError
				}
				return nil, nil
			default:
				t.Errorf("Unexpected exec call: %v",
					append([]string{name}, args...))
				return nil, errors.New("unreached")
			}
		}
	}

	setupExec(true, true)
	assert.True(t, strings.HasPrefix(cfgOVNImpl("", "").Error(), "get OVN config"))

	setupExec(false, true)
	assert.True(t, strings.HasPrefix(cfgOVNImpl("", "").Error(), "set OVN config"))
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

func ovsExecSetArgs(ip, leader string) []string {
	return []string{"set", "Open_vSwitch", ".",
		fmt.Sprintf("external_ids:ovn-remote=\"tcp:%s:6640\"", leader),
		fmt.Sprintf("external_ids:ovn-encap-ip=%q", ip),
		"external_ids:ovn-encap-type=stt",
		fmt.Sprintf("external_ids:api_server=\"http://%s:9000\"", leader),
		fmt.Sprintf("external_ids:system-id=%q", ip),
	}
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
