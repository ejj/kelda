package openflow

import (
	"errors"
	"testing"

	"github.com/quilt/quilt/minion/ovsdb"
	"github.com/quilt/quilt/minion/ovsdb/mocks"
	"github.com/stretchr/testify/assert"
)

func TestAddReplaceFlows(t *testing.T) {
	anErr := errors.New("err")
	ovsdb.Open = func() (ovsdb.Client, error) { return nil, anErr }
	assert.EqualError(t, ReplaceFlows(nil), "ovsdb-server connection: err")
	assert.EqualError(t, AddFlows(nil), "ovsdb-server connection: err")

	client := new(mocks.Client)
	ovsdb.Open = func() (ovsdb.Client, error) {
		return client, nil
	}

	var action string
	var flows []string
	ofctl = func(a string, f []string) error {
		action = a
		flows = f
		return nil
	}

	client.On("Disconnect").Return(nil)
	client.On("OpenFlowPorts").Return(map[string]int{}, nil)
	assert.NoError(t, ReplaceFlows(nil))
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")
	assert.Equal(t, "replace-flows", action)
	assert.Equal(t, allFlows(nil), flows)

	assert.NoError(t, AddFlows(nil))
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")

	assert.Equal(t, "add-flows", action)
	assert.Equal(t, containerFlows(nil), flows)

	ofctl = func(a string, f []string) error { return anErr }
	assert.EqualError(t, ReplaceFlows(nil), "ovs-ofctl: err")
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")

	assert.EqualError(t, AddFlows(nil), "ovs-ofctl: err")
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "OpenFlowPorts")
}

func TestAllFlows(t *testing.T) {
	t.Parallel()
	flows := allFlows([]container{
		{patch: 4, veth: 5, mac: "66:66:66:66:66:66"},
		{patch: 9, veth: 8, mac: "99:99:99:99:99:99"}})
	exp := append(staticFlows,
		"table=0,priority=1000,in_port=5,dl_src=66:66:66:66:66:66,"+
			"actions=load:0x1->NXM_NX_REG0[],load:0x5->NXM_NX_REG1[],"+
			"load:0x4->NXM_NX_REG2[],resubmit(,1)",
		"table=0,priority=1000,in_port=4,"+
			"actions=load:0x2->NXM_NX_REG0[],load:0x5->NXM_NX_REG1[],"+
			"load:0x4->NXM_NX_REG2[],resubmit(,1)",
		"table=2,priority=1000,dl_dst=66:66:66:66:66:66,actions=output:5",
		"table=0,priority=1000,in_port=8,dl_src=99:99:99:99:99:99,"+
			"actions=load:0x1->NXM_NX_REG0[],load:0x8->NXM_NX_REG1[],"+
			"load:0x9->NXM_NX_REG2[],resubmit(,1)",
		"table=0,priority=1000,in_port=9,"+
			"actions=load:0x2->NXM_NX_REG0[],load:0x8->NXM_NX_REG1[],"+
			"load:0x9->NXM_NX_REG2[],resubmit(,1)",
		"table=2,priority=1000,dl_dst=99:99:99:99:99:99,actions=output:8",
		"table=1,priority=850,dl_dst=ff:ff:ff:ff:ff:ff,actions=output:5,output:8")
	assert.Equal(t, exp, flows)
}

func TestResolveContainers(t *testing.T) {
	t.Parallel()

	res := resolveContainers(map[string]int{"a": 3, "b": 4}, []Container{
		{Veth: "a", Patch: "b", Mac: "mac"},
		{Veth: "c", Patch: "d", Mac: "mac2"}})
	assert.Equal(t, []container{{veth: 3, patch: 4, mac: "mac"}}, res)
}
