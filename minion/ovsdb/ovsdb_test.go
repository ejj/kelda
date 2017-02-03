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

	selectOp := []ovs.Operation{{
		Op:    "select",
		Table: "Logical_Switch",
		Where: newCondition("name", "==", "foo")}}
	api.On("Transact", "OVN_Northbound", selectOp).Return(nil, anErr).Once()
	assert.EqualError(t, odb.CreateLogicalSwitch("foo"),
		"transaction error: listing logical switches: err")

	api.On("Transact", "OVN_Northbound", selectOp).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{nil}}}, nil).Once()
	assert.EqualError(t, odb.CreateLogicalSwitch("foo"),
		"logical switch foo already exists")

	api.On("Transact", "OVN_Northbound", selectOp).Return(
		[]ovs.OperationResult{{}}, nil)

	op := []ovs.Operation{{
		Op:    "insert",
		Table: "Logical_Switch",
		Row:   map[string]interface{}{"name": "foo"}}}
	result := []ovs.OperationResult{{}}
	api.On("Transact", "OVN_Northbound", op).Return(result, nil).Once()
	assert.NoError(t, odb.CreateLogicalSwitch("foo"))

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr)
	err := odb.CreateLogicalSwitch("foo")
	assert.EqualError(t, err, "transaction error: creating switch foo: err")
}

func TestLogicalPorts(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	api.On("Transact", mock.Anything, mock.Anything).Return(nil, anErr).Once()
	_, err := odb.ListLogicalPorts()
	assert.EqualError(t, err, "transaction error: listing lports: err")

	api.On("Transact", "OVN_Northbound", []ovs.Operation{{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	}}).Return([]ovs.OperationResult{{Rows: nil}}, nil).Once()
	lports, err := odb.ListLogicalPorts()
	assert.Zero(t, lports)
	assert.NoError(t, err)

	r := map[string]interface{}{
		"_uuid":     []interface{}{"a", "b"},
		"name":      "name",
		"addresses": ""}
	api.On("Transact", "OVN_Northbound", []ovs.Operation{{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	}}).Return([]ovs.OperationResult{{
		Rows: []map[string]interface{}{r}}}, nil).Once()

	lports, err = odb.ListLogicalPorts()
	assert.Len(t, lports, 1)
	assert.Equal(t, "name", lports[0].Name)
	assert.NoError(t, err)
}

func TestCreateLogicalPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	addrs := newOvsSet([]string{"mac ip"})
	ops := []ovs.Operation{{
		Op:       "insert",
		Table:    "Logical_Switch_Port",
		Row:      map[string]interface{}{"name": "name", "addresses": addrs},
		UUIDName: "qlportadd",
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("ports", "insert", ovs.UUID{GoUUID: "qlportadd"}),
		},
		Where: newCondition("name", "==", "lswitch")}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.CreateLogicalPort("lswitch", "name", "mac", "ip")
	assert.EqualError(t, err,
		"transaction error: creating lport name on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateLogicalPort("lswitch", "name", "mac", "ip")
	assert.NoError(t, err)
}

func TestDeleteLogicalPort(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	lport := LPort{Name: "name", uuid: ovs.UUID{GoUUID: "uuid"}}
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
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.DeleteLogicalPort("lswitch", lport)
	assert.EqualError(t, err,
		"transaction error: deleting lport name on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.DeleteLogicalPort("lswitch", lport)
	assert.NoError(t, err)
}

func TestListACLs(t *testing.T) {
	t.Parallel()

	anErr := errors.New("err")
	api := new(mockTransact)
	odb := Client(client{api})

	ops := []ovs.Operation{{
		Op:    "select",
		Table: "ACL",
		Where: noCondition}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, anErr).Once()
	_, err := odb.ListACLs()
	assert.EqualError(t, err, "transaction error: listing ACLs: err")

	r := map[string]interface{}{
		"_uuid":     []interface{}{"a", "b"},
		"priority":  float64(1),
		"direction": "left",
		"match":     "match",
		"action":    "action",
		"log":       false}
	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{
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

func TestCreateACL(t *testing.T) {
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
		UUIDName: "qacladd",
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "insert", ovs.UUID{GoUUID: "qacladd"}),
		},
		Where: newCondition("name", "==", "lswitch"),
	}}

	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.CreateACL("lswitch", "direction", 1, "match", "action")
	assert.EqualError(t, err, "transaction error: creating ACL on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.CreateACL("lswitch", "direction", 1, "match", "action")
	assert.NoError(t, err)
}

func TestDeleteACL(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	acl := ACL{uuid: ovs.UUID{GoUUID: "uuid"}}
	ops := []ovs.Operation{{
		Op:    "delete",
		Table: "ACL",
		Where: newCondition("_uuid", "==", acl.uuid),
	}, {
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "delete", acl.uuid),
		},
		Where: newCondition("name", "==", "lswitch"),
	}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.DeleteACL("lswitch", acl)
	assert.EqualError(t, err, "transaction error: deleting ACL on lswitch: err")

	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{}, {}}, nil)
	err = odb.DeleteACL("lswitch", acl)
	assert.NoError(t, err)
}

func TestListAddressSets(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	ops := []ovs.Operation{{
		Op:    "select",
		Table: "Address_Set",
		Where: noCondition}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	_, err := odb.ListAddressSets()
	assert.EqualError(t, err, "transaction error: list address sets: err")

	r := map[string]interface{}{
		"name":      "name",
		"addresses": ""}
	api.On("Transact", "OVN_Northbound", ops).Return(
		[]ovs.OperationResult{{Rows: []map[string]interface{}{r}}}, nil).Once()
	res, err := odb.ListAddressSets()
	assert.NoError(t, err)
	assert.Equal(t, []AddressSet{{Name: "name", Addresses: []string{""}}}, res)
}

func TestCreateAddressSet(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	ops := []ovs.Operation{{
		Op:    "insert",
		Table: "Address_Set",
		Row: map[string]interface{}{
			"name":      "name",
			"addresses": newOvsSet([]string{})}}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.CreateAddressSet("name", nil)
	assert.EqualError(t, err, "transaction error: creating address set: err")

	api.On("Transact", "OVN_Northbound", ops).Return([]ovs.OperationResult{{}}, nil)
	err = odb.CreateAddressSet("name", nil)
	assert.NoError(t, err)
}

func TestDeleteAddressSet(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	ops := []ovs.Operation{{
		Op:    "delete",
		Table: "Address_Set",
		Where: newCondition("name", "==", "name")}}
	api.On("Transact", "OVN_Northbound", ops).Return(nil, errors.New("err")).Once()
	err := odb.DeleteAddressSet("name")
	assert.EqualError(t, err, "transaction error: deleting address set: err")

	api.On("Transact", "OVN_Northbound", ops).Return([]ovs.OperationResult{{}}, nil)
	err = odb.DeleteAddressSet("name")
	assert.NoError(t, err)
}

func TestOpenFlowPorts(t *testing.T) {
	t.Parallel()

	api := new(mockTransact)
	odb := Client(client{api})

	ops := []ovs.Operation{{
		Op:    "select",
		Table: "Interface",
		Where: noCondition}}
	api.On("Transact", "Open_vSwitch", ops).Return(nil, errors.New("err")).Once()
	_, err := odb.OpenFlowPorts()
	assert.EqualError(t, err, "select interface error: err")

	res := []ovs.OperationResult{{Rows: []map[string]interface{}{
		{},
		{"name": "bad"},
		{"name": "name", "ofport": float64(12)}}}}
	api.On("Transact", "Open_vSwitch", ops).Return(res, nil).Once()
	mp, err := odb.OpenFlowPorts()
	assert.NoError(t, err)
	assert.Equal(t, map[string]int{"name": 12}, mp)
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

func TestLPortSlice(t *testing.T) {
	t.Parallel()
	slice := LPortSlice([]LPort{{Name: "name"}})
	assert.Equal(t, slice[0], slice.Get(0))
	assert.Equal(t, 1, slice.Len())
}
