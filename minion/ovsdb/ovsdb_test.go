package ovsdb

import (
	"errors"
	"testing"

	ovs "github.com/socketplane/libovsdb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCreateLogicalSwitch(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	op := ovs.Operation{
		Op:    "insert",
		Table: "Logical_Switch",
		Row:   map[string]interface{}{"name": "foo"}}
	result := []ovs.OperationResult{{}}
	api.On("Transact", "OVN_Northbound", op).Return(result, nil).Once()
	assert.NoError(t, odb.CreateLogicalSwitch("foo"))

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr)
	err := odb.CreateLogicalSwitch("foo")
	assert.EqualError(t, err, "transaction error: creating switch foo: err")
}

func TestLogicalSwitchExists(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	selectOp := ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: newCondition("name", "==", "foo")}
	api.On("Transact", "OVN_Northbound", selectOp).Return(nil, anErr).Once()
	_, err := odb.LogicalSwitchExists("foo")
	assert.EqualError(t, err,
		"transaction error: listing logical switch: err")

	api.On("Transact", "OVN_Northbound", selectOp).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{nil}}}, nil).Once()
	exists, err := odb.LogicalSwitchExists("foo")
	assert.NoError(t, err)
	assert.True(t, exists)

	api.On("Transact", "OVN_Northbound", selectOp).Return(nil, nil).Once()
	exists, err = odb.LogicalSwitchExists("foo")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestSwitchPorts(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr).Once()
	_, err := odb.ListSwitchPorts()
	assert.EqualError(t, err, "transaction error: listing switch ports: err")

	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	}).Return([]ovs.OperationResult{{Rows: nil}}, nil).Once()
	lports, err := odb.ListSwitchPorts()
	assert.Zero(t, lports)
	assert.NoError(t, err)

	r := map[string]interface{}{
		"_uuid":     []interface{}{"a", "b"},
		"name":      "name",
		"addresses": "mac ip",
		"options": []interface{}{"map", []interface{}{
			[]interface{}{"foo", "bar"},
		}},
		"type": "router",
	}
	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	}).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	lports, err = odb.ListSwitchPorts()
	assert.NoError(t, err)
	assert.Equal(t, []SwitchPort{
		{
			uuid:      ovs.UUID{GoUUID: "b"},
			Name:      "name",
			Type:      "router",
			Addresses: []string{"mac ip"},
			Options:   map[string]string{"foo": "bar"},
		},
	}, lports)

	r = map[string]interface{}{
		"_uuid":     []interface{}{"a", "b"},
		"name":      "",
		"addresses": "",
		"options":   "brokenMap",
		"type":      "",
	}
	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	}).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	_, err = odb.ListSwitchPorts()
	assert.EqualError(t, err,
		"malformed switch port: malformed options: "+
			"ovs map outermost layer invalid")
}

func TestCreateSwitchPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	port := SwitchPort{
		Name:      "name",
		Addresses: []string{"mac ip"},
		Type:      "router",
		Options:   map[string]string{"foo": "bar"},
	}
	ops := []ovs.Operation{{
		Op:    "insert",
		Table: "Logical_Switch_Port",
		Row: map[string]interface{}{
			"name":      "name",
			"addresses": newOvsSet([]string{"mac ip"}),
			"type":      "router",
			"options":   newOvsMap(map[string]string{"foo": "bar"}),
		},
		UUIDName: "qlsportadd",
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("ports", "insert", ovs.UUID{GoUUID: "qlsportadd"}),
		},
		Where: newCondition("name", "==", "lswitch")}}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.CreateSwitchPort("lswitch", port)
	assert.EqualError(t, err,
		"transaction error: creating switch port name on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateSwitchPort("lswitch", port)
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestDeleteSwitchPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	lport := SwitchPort{Name: "name", uuid: ovs.UUID{GoUUID: "uuid"}}
	ops := []ovs.Operation{{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: newCondition("_uuid", "==", ovs.UUID{GoUUID: "uuid"}),
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{newMutation("ports", "delete",
			ovs.UUID{GoUUID: "uuid"})},
		Where: newCondition("name", "==", "lswitch")}}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.DeleteSwitchPort("lswitch", lport)
	assert.EqualError(t, err,
		"transaction error: deleting switch port name on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.DeleteSwitchPort("lswitch", lport)
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestListSwitchPort(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr).Once()
	_, err := odb.ListSwitchPort("name")
	assert.EqualError(t, err, "transaction error: listing switch ports: err")

	r := map[string]interface{}{
		"_uuid": []interface{}{"a", "b"},
		"name":  "name",
		"type":  "",
		"options": []interface{}{"map", []interface{}{
			[]interface{}{"foo", "bar"},
		}},
		"addresses": []interface{}{"set", []interface{}{"addresses"}},
	}
	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: newCondition("name", "==", "name"),
	}).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	lport, err := odb.ListSwitchPort("name")
	assert.NoError(t, err)
	assert.Equal(t, SwitchPort{
		uuid:      ovs.UUID{GoUUID: "b"},
		Name:      "name",
		Options:   map[string]string{"foo": "bar"},
		Addresses: []string{"addresses"},
	}, lport)
}

func TestUpdateSwitchPortAddresses(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr).Once()
	err := odb.UpdateSwitchPortAddresses("lport", []string{"addresses"})
	assert.EqualError(t, err, "transaction error: updating switch port lport: err")

	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "update",
		Table: "Logical_Switch_Port",
		Where: newCondition("name", "==", "lport"),
		Row: map[string]interface{}{
			"addresses": newOvsSet([]string{"addresses"}),
		},
	}).Return([]ovs.OperationResult{{
		Rows: nil}}, nil).Once()

	err = odb.UpdateSwitchPortAddresses("lport", []string{"addresses"})
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestCreateLogicalRouter(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	op := ovs.Operation{
		Op:    "insert",
		Table: "Logical_Router",
		Row:   map[string]interface{}{"name": "foo"}}
	result := []ovs.OperationResult{{}}
	api.On("Transact", "OVN_Northbound", op).Return(result, nil).Once()
	assert.NoError(t, odb.CreateLogicalRouter("foo"))

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr)
	err := odb.CreateLogicalRouter("foo")
	assert.EqualError(t, err, "transaction error: creating router foo: err")
}

func TestLogicalRouterExists(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	selectOp := ovs.Operation{
		Op:    "select",
		Table: "Logical_Router",
		Where: newCondition("name", "==", "foo")}
	api.On("Transact", "OVN_Northbound", selectOp).Return(nil, anErr).Once()
	_, err := odb.LogicalRouterExists("foo")
	assert.EqualError(t, err,
		"transaction error: listing logical router: err")

	api.On("Transact", "OVN_Northbound", selectOp).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{nil}}}, nil).Once()
	exists, err := odb.LogicalRouterExists("foo")
	assert.NoError(t, err)
	assert.True(t, exists)

	api.On("Transact", "OVN_Northbound", selectOp).Return(nil, nil).Once()
	exists, err = odb.LogicalRouterExists("foo")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestListRouterPorts(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr).Once()
	_, err := odb.ListRouterPorts()
	assert.EqualError(t, err, "transaction error: listing logical router ports: err")

	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Router_Port",
		Where: noCondition,
	}).Return([]ovs.OperationResult{{Rows: nil}}, nil).Once()
	lports, err := odb.ListRouterPorts()
	assert.Zero(t, lports)
	assert.NoError(t, err)

	r := map[string]interface{}{
		"_uuid":    []interface{}{"a", "b"},
		"name":     "name",
		"mac":      "mac",
		"networks": "0.0.0.0/0",
	}
	api.On("Transact", "OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Router_Port",
		Where: noCondition,
	}).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	lports, err = odb.ListRouterPorts()
	assert.NoError(t, err)
	assert.Equal(t, []RouterPort{
		{
			uuid:     ovs.UUID{GoUUID: "b"},
			Name:     "name",
			MAC:      "mac",
			Networks: []string{"0.0.0.0/0"},
		},
	}, lports)
}

func TestCreateRouterPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	port := RouterPort{
		Name:     "name",
		MAC:      "mac",
		Networks: []string{"network"},
	}
	ops := []ovs.Operation{{
		Op:    "insert",
		Table: "Logical_Router_Port",
		Row: map[string]interface{}{
			"name":     "name",
			"networks": newOvsSet([]string{"network"}),
			"mac":      "mac",
		},
		UUIDName: "qlrportadd",
	}, {
		Op:    "mutate",
		Table: "Logical_Router",
		Mutations: []interface{}{
			newMutation("ports", "insert", ovs.UUID{GoUUID: "qlrportadd"}),
		},
		Where: newCondition("name", "==", "lrouter")}}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.CreateRouterPort("lrouter", port)
	assert.EqualError(t, err,
		"transaction error: creating logical router port name on lrouter: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateRouterPort("lrouter", port)
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestDeleteRouterPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	lport := RouterPort{Name: "name", uuid: ovs.UUID{GoUUID: "uuid"}}
	ops := []ovs.Operation{{
		Op:    "delete",
		Table: "Logical_Router_Port",
		Where: newCondition("_uuid", "==", ovs.UUID{GoUUID: "uuid"}),
	}, {
		Op:    "mutate",
		Table: "Logical_Router",
		Mutations: []interface{}{newMutation("ports", "delete",
			ovs.UUID{GoUUID: "uuid"})},
		Where: newCondition("name", "==", "lrouter")}}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.DeleteRouterPort("lrouter", lport)
	assert.EqualError(t, err,
		"transaction error: deleting logical router port name on lrouter: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.DeleteRouterPort("lrouter", lport)
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestListACLs(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	op := ovs.Operation{
		Op:    "select",
		Table: "ACL",
		Where: noCondition}
	api.On("Transact", "OVN_Northbound", op).Return(nil, anErr).Once()
	_, err := odb.ListACLs()
	assert.EqualError(t, err, "transaction error: listing ACLs: err")

	r := map[string]interface{}{
		"_uuid":     []interface{}{"a", "b"},
		"priority":  float64(1),
		"direction": "left",
		"match":     "match",
		"action":    "action",
		"log":       false}
	api.On("Transact", "OVN_Northbound", op).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	acls, err := odb.ListACLs()
	assert.NoError(t, err)
	acls[0].uuid = ovs.UUID{}
	assert.Equal(t, ACL{Core: ACLCore{
		Priority:  1,
		Direction: "left",
		Match:     "match",
		Action:    "action"}}, acls[0])
}

func TestCreateACLs(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	aclRow := map[string]interface{}{
		"priority":  1,
		"direction": "direction",
		"match":     "match",
		"action":    "action",
		"log":       false}
	ops := []ovs.Operation{{
		Op:       "insert",
		Table:    "ACL",
		Row:      aclRow,
		UUIDName: "qacladd0",
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "insert", ovs.UUID{GoUUID: "qacladd0"}),
		},
		Where: newCondition("name", "==", "lswitch"),
	}}

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.CreateACLs("lswitch", []ACLCore{{
		Priority:  1,
		Direction: "direction",
		Match:     "match",
		Action:    "action"}})
	assert.EqualError(t, err, "transaction error: creating ACL on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateACLs("lswitch", []ACLCore{{
		Priority:  1,
		Direction: "direction",
		Match:     "match",
		Action:    "action"}})
	assert.NoError(t, err)
}

func TestDeleteACLs(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	acls := []ACL{{
		uuid: ovs.UUID{GoUUID: "uuid1"},
	}, {
		uuid: ovs.UUID{GoUUID: "uuid2"},
	}}

	ops := []ovs.Operation{{
		Op:    "delete",
		Table: "ACL",
		Where: newCondition("_uuid", "==", acls[0].uuid),
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "delete", acls[0].uuid),
		},
		Where: newCondition("name", "==", "lswitch"),
	}, {
		Op:    "delete",
		Table: "ACL",
		Where: newCondition("_uuid", "==", acls[1].uuid),
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "delete", acls[1].uuid),
		},
		Where: newCondition("name", "==", "lswitch"),
	}}

	api.On("Transact", "OVN_Northbound", ops[0], ops[1], ops[2], ops[3]).Return(
		nil, errors.New("err")).Once()
	err := odb.DeleteACLs("lswitch", acls)
	assert.EqualError(t, err, "transaction error: deleting ACL on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1], ops[2], ops[3]).Return(
		[]ovs.OperationResult{{}, {}, {}, {}}, nil)
	err = odb.DeleteACLs("lswitch", acls)
	assert.NoError(t, err)
}

func TestOpenFlowPorts(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	op := ovs.Operation{
		Op:    "select",
		Table: "Interface",
		Where: noCondition}
	api.On("Transact", "Open_vSwitch", op).Return(nil, errors.New("err")).Once()
	_, err := odb.OpenFlowPorts()
	assert.EqualError(t, err, "select interface error: err")

	res := []ovs.OperationResult{{Rows: []map[string]interface{}{
		{},
		{"name": "bad"},
		{"name": "name", "ofport": float64(12)}}}}
	api.On("Transact", "Open_vSwitch", op).Return(res, nil).Once()
	mp, err := odb.OpenFlowPorts()
	assert.NoError(t, err)
	assert.Equal(t, map[string]int{"name": 12}, mp)
}

func TestListLoadBalancers(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	op := ovs.Operation{
		Op:    "select",
		Table: "Load_Balancer",
		Where: noCondition}
	api.On("Transact", "OVN_Northbound", op).Return(nil, errors.New("err")).Once()
	_, err := odb.ListLoadBalancers()
	assert.EqualError(t, err, "transaction error: listing load balancers: err")

	r := map[string]interface{}{
		"_uuid": []interface{}{"a", "b"},
		"name":  "name",
		"vips": []interface{}{"map", []interface{}{
			[]interface{}{"vip", "addrs"},
		}},
	}
	api.On("Transact", "OVN_Northbound", op).Return(
		[]ovs.OperationResult{{Rows: []map[string]interface{}{r}}}, nil).Once()
	res, err := odb.ListLoadBalancers()
	assert.NoError(t, err)
	assert.Equal(t, []LoadBalancer{
		{
			uuid: ovs.UUID{GoUUID: "b"},
			Name: "name",
			VIPs: map[string]string{"vip": "addrs"},
		},
	}, res)
}

func TestCreateLoadBalancer(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	mut := newMutation("load_balancer", "insert", ovs.UUID{GoUUID: "qlbadd"})
	ops := []ovs.Operation{
		{
			Op:    "insert",
			Table: "Load_Balancer",
			Row: map[string]interface{}{
				"name": "name",
				"vips": newOvsMap(map[string]string{"vip": "addrs"}),
			},
			UUIDName: "qlbadd",
		},
		{
			Op:        "mutate",
			Table:     "Logical_Switch",
			Mutations: []interface{}{mut},
			Where:     newCondition("name", "==", "lswitch"),
		},
	}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.CreateLoadBalancer("lswitch", "name",
		map[string]string{"vip": "addrs"})
	assert.EqualError(t, err,
		"transaction error: creating load balancer on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateLoadBalancer("lswitch", "name",
		map[string]string{"vip": "addrs"})
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestDeleteLoadBalancer(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	mut := newMutation("load_balancer", "delete", ovs.UUID{GoUUID: "foo"})
	ops := []ovs.Operation{
		{
			Op:    "delete",
			Table: "Load_Balancer",
			Where: newCondition("_uuid", "==", ovs.UUID{GoUUID: "foo"}),
		},
		{
			Op:        "mutate",
			Table:     "Logical_Switch",
			Mutations: []interface{}{mut},
			Where:     newCondition("name", "==", "lswitch"),
		},
	}
	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		nil, errors.New("err")).Once()
	err := odb.DeleteLoadBalancer("lswitch",
		LoadBalancer{
			uuid: ovs.UUID{GoUUID: "foo"},
		},
	)
	assert.EqualError(t, err,
		"transaction error: deleting load balancer on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops[0], ops[1]).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.DeleteLoadBalancer("lswitch",
		LoadBalancer{
			uuid: ovs.UUID{GoUUID: "foo"},
		},
	)
	assert.NoError(t, err)

	api.AssertExpectations(t)
}

func TestOvsStringSetToSlice(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"b"}, ovsStringSetToSlice("b"))
	assert.Equal(t, []string{"b"},
		ovsStringSetToSlice([]interface{}{"set", []interface{}{"b"}}))
}

func TestErrorCheck(t *testing.T) {
	t.Parallel()
	assert.EqualError(t, errorCheck(nil, 3), "mismatched responses and operations")
	assert.EqualError(t, errorCheck([]ovs.OperationResult{{Error: "foo"}}, 1),
		"operation 0 failed due to error: foo: ")
	assert.NoError(t, errorCheck([]ovs.OperationResult{{}}, 1))
}

func TestSwitchPortSlice(t *testing.T) {
	t.Parallel()
	slice := SwitchPortSlice([]SwitchPort{{Name: "name"}})
	assert.Equal(t, slice[0], slice.Get(0))
	assert.Equal(t, 1, slice.Len())
}
