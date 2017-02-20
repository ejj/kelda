package plugin

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/quilt/quilt/minion/ipdef"
	"github.com/quilt/quilt/minion/network/openflow"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"

	dnet "github.com/docker/go-plugins-helpers/network"
)

var (
	zero  = "000000000000000000000000000000000000000000000000"
	one   = "111111111111111111111111111111111111111111111111"
	links = map[string]netlink.Link{}
)

func mockLinkAdd(l netlink.Link) error {
	name := l.Attrs().Name
	if len(name) >= syscall.IFNAMSIZ {
		panic(fmt.Sprintf("len(\"%s\") >= %d", name, syscall.IFNAMSIZ))
	}

	if _, ok := links[name]; ok {
		return fmt.Errorf("veth exists: %s", name)
	}

	links[name] = l
	return nil
}

func mockLinkDel(l netlink.Link) error {
	name := l.Attrs().Name
	if _, ok := links[name]; !ok {
		return fmt.Errorf("del: no such veth: %s", name)
	}
	delete(links, name)
	return nil
}

func mockLinkByName(name string) (netlink.Link, error) {
	if _, ok := links[name]; !ok {
		return nil, fmt.Errorf("byName: no such veth: %s", name)
	}
	return links[name], nil
}

func mockLinkSetUp(link netlink.Link) error {
	return nil
}

func setup() {
	links = map[string]netlink.Link{}
	linkAdd = mockLinkAdd
	linkDel = mockLinkDel
	linkSetUp = mockLinkSetUp
	linkByName = mockLinkByName
}

func TestGetCapabilities(t *testing.T) {
	setup()

	d := driver{}
	resp, err := d.GetCapabilities()
	assert.NoError(t, err)

	exp := dnet.CapabilitiesResponse{Scope: dnet.LocalScope}
	assert.Equal(t, exp, *resp)
}

func TestCreateEndpoint(t *testing.T) {
	setup()

	anErr := errors.New("err")

	vsctl = func(a [][]string) error { return anErr }
	ofctl = func(c openflow.Container) error { return anErr }

	req := &dnet.CreateEndpointRequest{}
	req.EndpointID = zero
	req.Interface = &dnet.EndpointInterface{
		MacAddress: "00:00:00:00:00:00",
	}

	d := driver{}
	_, err := d.CreateEndpoint(req)
	assert.EqualError(t, err, "invalid IP: ")

	req.Interface.Address = "10.1.0.1/8"

	linkAdd = func(link netlink.Link) error { return anErr }
	_, err = d.CreateEndpoint(req)
	assert.EqualError(t, err, "failed to create veth: err")

	setup()
	linkByName = func(eid string) (netlink.Link, error) { return nil, anErr }
	_, err = d.CreateEndpoint(req)
	assert.EqualError(t, err, "failed to find link 000000000000000: err")

	setup()
	linkSetUp = func(link netlink.Link) error { return anErr }
	_, err = d.CreateEndpoint(req)
	assert.EqualError(t, err, "failed to bring up link 000000000000000: err")

	setup()
	req.Interface.MacAddress = ""
	_, err = d.CreateEndpoint(req)
	assert.EqualError(t, err, "ovs-vsctl: err")

	setup()

	var args [][]string
	vsctl = func(a [][]string) error {
		args = a
		return nil
	}

	expResp := dnet.EndpointInterface{
		MacAddress: ipdef.IPStrToMac("10.1.0.1"),
	}

	resp, err := d.CreateEndpoint(req)
	assert.NoError(t, err)
	assert.Equal(t, expResp, *resp.Interface)
	assert.Equal(t, [][]string{
		{"add-port", "quilt-int", "000000000000000"},
		{"add-port", "quilt-int", "q_0000000000000"},
		{"set", "Interface", "q_0000000000000", "type=patch",
			"options:peer=br_000000000000"},
		{"add-port", "br-int", "br_000000000000"},
		{"set", "Interface", "br_000000000000", "type=patch",
			"options:peer=q_0000000000000",
			"external-ids:attached-mac=02:00:0a:01:00:01",
			"external-ids:iface-id=10.1.0.1"}}, args)

	req.EndpointID = one
	req.Interface.Address = "10.1.0.2/8"
	expResp.MacAddress = ipdef.IPStrToMac("10.1.0.2")
	resp, err = d.CreateEndpoint(req)
	assert.NoError(t, err)
	assert.Equal(t, expResp, *resp.Interface)
}

func TestDeleteEndpoint(t *testing.T) {
	var args [][]string

	setup()

	vsctl = func(a [][]string) error {
		args = a
		return nil
	}

	links["foo"] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "foo"}}

	req := &dnet.DeleteEndpointRequest{EndpointID: "foo"}

	d := driver{}
	err := d.DeleteEndpoint(req)
	assert.NoError(t, err)

	expOvsArgs := [][]string{
		{"del-port", "quilt-int", "foo"},
		{"del-port", "quilt-int", "q_foo"},
		{"del-port", "br-int", "br_foo"}}
	assert.Equal(t, expOvsArgs, args)

	err = d.DeleteEndpoint(req)
	assert.EqualError(t, err, "failed to find link foo: byName: no such veth: foo")

	links["foo"] = &netlink.Dummy{}
	err = d.DeleteEndpoint(req)
	assert.EqualError(t, err, "failed to delete link foo: del: no such veth: ")

	vsctl = func(a [][]string) error { return errors.New("err") }
	err = d.DeleteEndpoint(req)
	assert.EqualError(t, err, "ovs-vsctl: err")
}

func TestEndpointInfo(t *testing.T) {
	setup()

	d := driver{}
	_, err := d.EndpointInfo(&dnet.InfoRequest{EndpointID: "foo"})
	assert.EqualError(t, err, "byName: no such veth: foo")

	links["foo"] = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "foo"}}
	resp, err := d.EndpointInfo(&dnet.InfoRequest{EndpointID: "foo"})
	assert.NoError(t, err)
	assert.Equal(t, &dnet.InfoResponse{}, resp)
}

func TestJoin(t *testing.T) {
	t.Parallel()

	d := driver{}
	jreq := &dnet.JoinRequest{EndpointID: zero, SandboxKey: "/test/docker0"}
	resp, err := d.Join(jreq)
	assert.NoError(t, err)
	assert.Equal(t, &dnet.JoinResponse{
		InterfaceName: dnet.InterfaceName{
			SrcName:   "tmp_00000000000",
			DstPrefix: "eth"},
		Gateway: "10.0.0.1"}, resp)
}

func TestLeave(t *testing.T) {
	setup()

	d := driver{}
	_, err := d.Join(&dnet.JoinRequest{EndpointID: zero, SandboxKey: "/test/docker0"})
	assert.NoError(t, err)

	err = d.Leave(&dnet.LeaveRequest{EndpointID: zero})
	assert.NoError(t, err)
}
