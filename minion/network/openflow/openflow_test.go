package openflow

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/minion/ovsdb/mocks"
	"github.com/stretchr/testify/assert"
)

func TestAddReplaceFlows(t *testing.T) {
	anErr := errors.New("err")
	ovsdb.Open = func() (ovsdb.Client, error) { return nil, anErr }
	assert.EqualError(t, replaceFlows(nil), "ovsdb-server connection: err")
	assert.EqualError(t, AddFlows(nil), "ovsdb-server connection: err")

	client := new(mocks.Client)
	ovsdb.Open = func() (ovsdb.Client, error) {
		return client, nil
	}

	actionsToFlows := map[string][]string{}
	diffFlowsShouldErr := true
	ofctl = func(a string, f []string) error {
		actionsToFlows[a] = f
		if a == "diff-flows" && diffFlowsShouldErr {
			return errors.New("flows differ")
		}
		return nil
	}

	client.On("Disconnect").Return(nil)
	client.On("OpenFlowPorts").Return(map[string]int{}, nil)
	assert.NoError(t, replaceFlows(nil))
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")
	assert.Equal(t, map[string][]string{
		"diff-flows":    allFlows(nil),
		"replace-flows": allFlows(nil),
	}, actionsToFlows)

	// Test that we don't call replace-flows when there are no differences.
	actionsToFlows = map[string][]string{}
	diffFlowsShouldErr = false
	assert.NoError(t, replaceFlows(nil))
	assert.Equal(t, map[string][]string{
		"diff-flows": allFlows(nil),
	}, actionsToFlows)

	actionsToFlows = map[string][]string{}
	assert.NoError(t, AddFlows(nil))
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")

	assert.Equal(t, map[string][]string{
		"add-flows": nil,
	}, actionsToFlows)

	ofctl = func(a string, f []string) error { return anErr }
	assert.EqualError(t, replaceFlows(nil), "ovs-ofctl: err")
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")

	assert.EqualError(t, AddFlows(nil), "ovs-ofctl: err")
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")
}

func TestAllFlows(t *testing.T) {
	t.Parallel()
	flows := allFlows([]container{{
		patchPort: 4,
		vethPort:  5,
		Container: Container{
			IP:    "6.7.8.9",
			Mac:   "66:66:66:66:66:66",
			ToPub: map[int]struct{}{5: {}}},
	}, {
		patchPort: 9,
		vethPort:  8,
		Container: Container{
			IP:      "9.8.7.6",
			Mac:     "99:99:99:99:99:99",
			FromPub: map[int]struct{}{8: {}}}}})
	exp := append(staticFlows,
		"table=0,in_port=5,dl_src=66:66:66:66:66:66,"+
			"actions=load:0x4->NXM_NX_REG0[],resubmit(,1)",
		"table=0,in_port=4,actions=output:5",
		"table=2,priority=900,arp,dl_dst=66:66:66:66:66:66,action=output:5",
		"table=2,priority=800,ip,dl_dst=66:66:66:66:66:66,nw_src=10.0.0.1,"+
			"action=output:5",
		"table=2,priority=500,tcp,dl_dst=66:66:66:66:66:66,ip_dst=6.7.8.9,"+
			"tp_src=5,actions=output:5",
		"table=2,priority=500,udp,dl_dst=66:66:66:66:66:66,ip_dst=6.7.8.9,"+
			"tp_src=5,actions=output:5",
		"table=3,priority=500,tcp,dl_src=66:66:66:66:66:66,ip_src=6.7.8.9,"+
			"tp_dst=5,actions=output:LOCAL",
		"table=3,priority=500,udp,dl_src=66:66:66:66:66:66,ip_src=6.7.8.9,"+
			"tp_dst=5,actions=output:LOCAL",
		"table=0,in_port=8,dl_src=99:99:99:99:99:99,"+
			"actions=load:0x9->NXM_NX_REG0[],resubmit(,1)",
		"table=0,in_port=9,actions=output:8",
		"table=2,priority=900,arp,dl_dst=99:99:99:99:99:99,action=output:8",
		"table=2,priority=800,ip,dl_dst=99:99:99:99:99:99,nw_src=10.0.0.1,"+
			"action=output:8",
		"table=2,priority=500,tcp,dl_dst=99:99:99:99:99:99,ip_dst=9.8.7.6,"+
			"tp_dst=8,actions=output:8",
		"table=2,priority=500,udp,dl_dst=99:99:99:99:99:99,ip_dst=9.8.7.6,"+
			"tp_dst=8,actions=output:8",
		"table=3,priority=500,tcp,dl_src=99:99:99:99:99:99,ip_src=9.8.7.6,"+
			"tp_src=8,actions=output:LOCAL",
		"table=3,priority=500,udp,dl_src=99:99:99:99:99:99,ip_src=9.8.7.6,"+
			"tp_src=8,actions=output:LOCAL",
		"table=2,priority=1000,dl_dst=ff:ff:ff:ff:ff:ff,"+
			"actions=output:5,output:8")
	assert.Equal(t, exp, flows)
}

func TestResolveContainers(t *testing.T) {
	t.Parallel()

	res := resolveContainers(map[string]int{"a": 3, "b": 4}, []Container{
		{Veth: "a", Patch: "b", Mac: "mac"},
		{Veth: "c", Patch: "d", Mac: "mac2"}})
	assert.Equal(t, []container{{
		vethPort:  3,
		patchPort: 4,
		Container: Container{Veth: "a", Patch: "b", Mac: "mac"}}}, res)
}
