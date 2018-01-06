package cni

import (
	"encoding/json"
	"errors"
	"net"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/nl/nlmock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestCmdDelResult(t *testing.T) {
	anErr := errors.New("err")
	mk := nlmock.I{}
	nl.N = &mk

	var cmd []string
	var execRunError error
	execRun = func(name string, args ...string) ([]byte, error) {
		cmd = append([]string{name}, args...)
		return []byte("output"), execRunError
	}
	args := skel.CmdArgs{ContainerID: "container-id"}

	mockPorts := []string{"port1", "port2"}
	var listPortsError error
	listPortsByTag = func(key, val string) ([]string, error) {
		if key == containerIDTag && val == args.ContainerID {
			return mockPorts, listPortsError
		}
		assert.FailNow(t, "unexpected call to listPortsByTag: key: %s, val: %s",
			key, val)
		return nil, errors.New("unreached")
	}

	// Fail to delete the OVS ports because we can't look them up in OVS.
	listPortsError = anErr
	err := cmdDel(&args)
	assert.EqualError(t, err, "list OVS ports: err")

	// Fail to delete the OVS ports because the exec call to ovs-vsctl fails.
	listPortsError = nil
	execRunError = anErr
	err = cmdDel(&args)
	assert.EqualError(t, err, "failed to teardown OVS ports: err (output)")

	// Successfully delete the OVS ports, but not the link because we can't
	// find it.
	execRunError = nil
	mk.On("LinkByAlias", args.ContainerID).Once().Return(nil, anErr)
	err = cmdDel(&args)
	assert.EqualError(t, err, "failed to find outer veth: err")
	assert.Equal(t, []string{"ovs-vsctl",
		"--", "del-port", "port1",
		"--", "del-port", "port2",
	}, cmd)

	// Still unable to delete the link. This time, because the delete call fails.
	link := &netlink.GenericLink{}
	mk.On("LinkByAlias", args.ContainerID).Return(link, nil)
	mk.On("LinkDel", link).Once().Return(anErr)
	err = cmdDel(&args)
	assert.EqualError(t, err, "failed to delete veth : err")

	// Successfully delete the link.
	mk.On("LinkDel", link).Return(nil)
	err = cmdDel(&args)
	assert.NoError(t, err)
}

func TestCmdAddResult(t *testing.T) {
	anErr := errors.New("err")
	mk := nlmock.I{}
	nl.N = &mk

	args := skel.CmdArgs{
		ContainerID: "container-id",
		IfName:      "eth0",
	}
	_, err := cmdAddResult(&args)
	assert.EqualError(t, err, "failed to find pod name in arguments")

	getPodAnnotations = func(podName string) (map[string]string, error) {
		return nil, anErr
	}
	args.Args = "K8S_POD_NAME=podName;"
	_, err = cmdAddResult(&args)
	assert.EqualError(t, err, "err")

	getPodAnnotations = func(podName string) (map[string]string, error) {
		return map[string]string{"keldaIP": "1.2.3.4"}, nil
	}
	mk.On("AddVeth", "1.2.3.4", args.ContainerID, "-1.2.3.4", mtu).
		Once().Return(anErr)
	_, err = cmdAddResult(&args)
	assert.EqualError(t, err, "failed to create veth 1.2.3.4: err")

	mk.On("AddVeth", "1.2.3.4", args.ContainerID, "-1.2.3.4", mtu).Return(nil)
	mk.On("LinkByName", mock.Anything).Once().Return(nil, anErr)
	_, err = cmdAddResult(&args)
	assert.EqualError(t, err, "failed to find link -1.2.3.4: err")

	mk.On("LinkByName", mock.Anything).Return(&netlink.GenericLink{}, nil)
	mk.On("GetNetns").Return(netns.NsHandle(0), nil)
	mk.On("CloseNsHandle", mock.Anything).Return(nil)
	mk.On("GetNetnsFromPath", mock.Anything).Return(netns.NsHandle(0), nil)
	mk.On("LinkSetNs", mock.Anything, mock.Anything).Return(nil)
	mk.On("SetNetns", mock.Anything).Return(nil)
	mk.On("LinkSetHardwareAddr", mock.Anything, mock.Anything).Return(nil)
	mk.On("AddrAdd", mock.Anything, mock.Anything).Return(nil)
	mk.On("LinkSetName", mock.Anything, mock.Anything).Return(nil)
	mk.On("LinkSetUp", mock.Anything).Return(nil)
	mk.On("RouteAdd", mock.Anything).Return(nil)

	execRun = func(name string, args ...string) ([]byte, error) {
		return []byte("output"), anErr
	}
	_, err = cmdAddResult(&args)
	assert.EqualError(t, err, "failed to setup OVS: failed to configure "+
		"OVSDB: err (output)")

	execRun = func(name string, args ...string) ([]byte, error) {
		return nil, nil
	}
	addFlows = func(containers []openflow.Container) error {
		return nil
	}
	result, err := cmdAddResult(&args)
	assert.NoError(t, err)

	js, err := json.Marshal(result)
	assert.NoError(t, err)
	assert.Equal(t, `{"cniVersion":"0.3.1","interfaces":[{"name":"eth0","mac":`+
		`"02:00:01:02:03:04"}],"ips":[{"version":"4","interface":0,"address":`+
		`"1.2.3.4/32","gateway":"10.0.0.1"}],"dns":{}}`, string(js))
}

func TestGrepPodName(t *testing.T) {
	_, err := grepPodName("missing")
	assert.EqualError(t, err, "failed to find pod name in arguments")

	name, err := grepPodName("K8S_POD_NAME=foo;")
	assert.NoError(t, err)
	assert.Equal(t, name, "foo")
}

func TestSetupOuterLink(t *testing.T) {
	mk := nlmock.I{}
	nl.N = &mk

	anErr := errors.New("err")
	name := "vethName"

	mk.On("LinkByName", name).Once().Return(nil, anErr)
	err := setupOuterLink(name)
	assert.EqualError(t, err, "failed to find link vethName: err")

	link := &netlink.GenericLink{}
	mk.On("LinkByName", name).Return(link, nil)
	mk.On("LinkSetUp", link).Once().Return(anErr)
	err = setupOuterLink(name)
	assert.EqualError(t, err, "failed to bring link up: err")

	mk.On("LinkSetUp", link).Return(nil)
	err = setupOuterLink(name)
	assert.NoError(t, err)
}

func TestSetupPod(t *testing.T) {
	mk := nlmock.I{}
	nl.N = &mk

	anErr := errors.New("err")
	ns := "/nspath"
	goalName := "eth0"
	vethName := "vethName"
	_, ip, err := net.ParseCIDR("1.2.3.4/32")
	assert.NoError(t, err)

	mac, err := net.ParseMAC("11:22:33:44:55:66")
	assert.NoError(t, err)

	mk.On("LinkByName", vethName).Once().Return(nil, anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to find link vethName: err")

	link := &netlink.GenericLink{}
	mk.On("LinkByName", vethName).Return(link, nil)
	mk.On("GetNetns").Once().Return(netns.NsHandle(0), anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to get current namespace handle: err")

	rootns := netns.NsHandle(1)
	mk.On("GetNetns").Return(rootns, nil)
	mk.On("GetNetnsFromPath", ns).Once().Return(netns.NsHandle(0), anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to open network namespace handle: err")

	nsh := netns.NsHandle(2)
	mk.On("CloseNsHandle", nsh).Return(nil)
	mk.On("GetNetnsFromPath", ns).Return(nsh, nil)
	mk.On("LinkSetNs", link, nsh).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to put link in pod namespace: err")

	mk.On("LinkSetNs", link, nsh).Return(nil)
	mk.On("SetNetns", nsh).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to enter pod network namespace: err")

	mk.On("SetNetns", rootns).Return(nil)
	mk.On("SetNetns", nsh).Return(nil)
	mk.On("LinkSetHardwareAddr", link, mac).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to set mac address: err")

	mk.On("LinkSetHardwareAddr", link, mac).Return(nil)
	mk.On("AddrAdd", link, *ip).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to set IP 1.2.3.4/32: err")

	mk.On("AddrAdd", link, *ip).Return(nil)
	mk.On("LinkSetName", link, goalName).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to set device name: err")

	mk.On("LinkSetName", link, goalName).Return(nil)
	mk.On("LinkSetUp", link).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to bring link up: err")

	mk.On("LinkSetUp", link).Return(nil)
	mk.On("RouteAdd", mock.Anything).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to add route: err")

	defaultRoute := nl.Route{Gw: ipdef.GatewayIP}
	mk.On("RouteAdd", nl.Route{
		Scope: nl.ScopeLink,
		Dst:   &ipdef.KeldaSubnet,
		Src:   ip.IP,
	}).Return(nil)
	mk.On("RouteAdd", defaultRoute).Once().Return(anErr)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.EqualError(t, err, "failed to add default route: err")

	mk.On("RouteAdd", defaultRoute).Return(nil)
	err = setupPod(ns, goalName, vethName, *ip, mac)
	assert.NoError(t, err)
}

func TestGetIPMac(t *testing.T) {
	mockLabels := map[string]string{}
	var mockErr error
	getPodAnnotations = func(podName string) (map[string]string, error) {
		return mockLabels, mockErr
	}

	mockErr = errors.New("err")
	_, _, err := getIPMac("pod")
	assert.EqualError(t, err, "err")

	mockErr = nil
	_, _, err = getIPMac("pod")
	assert.EqualError(t, err, "pod has no Kelda IP")

	mockLabels["keldaIP"] = "bad"
	_, _, err = getIPMac("pod")
	assert.EqualError(t, err, "invalid IP: bad")

	mockLabels["keldaIP"] = "1.2.3.4"
	ipnet, addr, err := getIPMac("pod")
	assert.NoError(t, err)

	assert.Equal(t, ipnet.String(), "1.2.3.4/32")
	assert.Equal(t, "02:00:01:02:03:04", addr.String())
}

func TestSetupOVS(t *testing.T) {
	execRun = func(name string, args ...string) ([]byte, error) {
		return []byte("output"), errors.New("execRun")
	}

	ip := net.ParseIP("1.2.3.4")
	mac, _ := net.ParseMAC(ipdef.IPToMac(ip))
	err := setupOVS("outer", ip, mac, "container-id")
	assert.EqualError(t, err, "failed to configure OVSDB: execRun (output)")

	var cmd []string
	execRun = func(name string, args ...string) ([]byte, error) {
		cmd = append([]string{name}, args...)
		return nil, nil
	}

	addFlows = func(containers []openflow.Container) error {
		return errors.New("addFlows")
	}

	err = setupOVS("outer", ip, mac, "container-id")
	assert.EqualError(t, err, "failed to populate OpenFlow tables: addFlows")

	var containers []openflow.Container
	addFlows = func(ofcs []openflow.Container) error {
		containers = ofcs
		return nil
	}

	err = setupOVS("outer", ip, mac, "container-id")
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"ovs-vsctl",

		"--", "add-port", "kelda-int", "outer",
		"external-ids:cni-container-id=container-id",

		"--", "add-port", "kelda-int", "q_1.2.3.4",
		"external-ids:cni-container-id=container-id",

		"--", "set", "Interface", "q_1.2.3.4", "type=patch",
		"options:peer=br_1.2.3.4",

		"--", "add-port", "br-int", "br_1.2.3.4",
		"external-ids:cni-container-id=container-id",

		"--", "set", "Interface", "br_1.2.3.4", "type=patch",
		"options:peer=q_1.2.3.4",
		"external-ids:attached-mac=02:00:01:02:03:04",
		"external-ids:iface-id=1.2.3.4"}, cmd)
	assert.Equal(t, []openflow.Container{{
		Veth:  "outer",
		Patch: "q_1.2.3.4",
		Mac:   "02:00:01:02:03:04",
		IP:    "1.2.3.4"}}, containers)
}
