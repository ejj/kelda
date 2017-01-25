//go:generate mockery -name=transact -inpkg
//go:generate mockery -name=Client

package ovsdb

import (
	"errors"
	"fmt"
	"math"

	ovs "github.com/socketplane/libovsdb"
)

type transact interface {
	Transact(db string, operation ...ovs.Operation) ([]ovs.OperationResult, error)
	Disconnect()
}

// Client is a connection to the ovsdb-server database.
type Client interface {
	CreateLogicalSwitch(lswitch string) error
	ListLogicalPorts() ([]LPort, error)
	CreateLogicalPort(lswitch, name, mac, ip string) error
	DeleteLogicalPort(lswitch string, lport LPort) error
	ListACLs() ([]ACL, error)
	CreateACL(lswitch, direction string, priority int, match, action string) error
	DeleteACL(lswitch string, ovsdbACL ACL) error
	ListAddressSets() ([]AddressSet, error)
	CreateAddressSet(name string, addresses []string) error
	DeleteAddressSet(name string) error
	OpenFlowPorts() (map[string]int, error)
	Disconnect()
}

type client struct {
	transact
}

// LPort is a logical port in OVN.
type LPort struct {
	uuid      ovs.UUID
	Name      string
	Addresses []string
}

// LPortSlice is a wrapper around []LPort so it can be used in joins
type LPortSlice []LPort

// Interface is a linux device attached to OVS.
type Interface struct {
	Name        string
	Peer        string
	AttachedMAC string
	IfaceID     string
	Bridge      string
	Type        string
}

// ACL is a firewall rule in OVN.
type ACL struct {
	uuid ovs.UUID
	Core ACLCore
	Log  bool
}

// ACLCore is the actual ACL rule that will be matched, without various OVSDB metadata
// found in ACL.
type ACLCore struct {
	Priority  int
	Direction string
	Match     string
	Action    string
}

type row map[string]interface{}
type mutation interface{}

// Base on RFC 7047, empty condition should return all rows from a table. However,
// libovsdb does not seem to support that yet. This is the simplist, most common solution
// to it.
var noCondition = []interface{}{ovs.NewCondition("_uuid", "!=", ovs.UUID{GoUUID: "_"})}

func newCondition(column, function string, value interface{}) []interface{} {
	return []interface{}{ovs.NewCondition(column, function, value)}
}

// Open creates a new Ovsdb connection.
// It's stored in a variable so we can mock it out for the unit tests.
var Open = func() (Client, error) {
	odb, err := ovs.Connect("127.0.0.1", 6640)
	return client{odb}, err
}

// CreateLogicalSwitch creates a new logical switch in OVN.
func (ovsdb client) CreateLogicalSwitch(lswitch string) error {
	check, err := ovsdb.Transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: newCondition("name", "==", lswitch),
	})
	if err != nil {
		return fmt.Errorf("transaction error: listing logical switches: %s", err)
	}
	if len(check[0].Rows) > 0 {
		return fmt.Errorf("logical switch %s already exists", lswitch)
	}

	insertOp := ovs.Operation{
		Op:    "insert",
		Table: "Logical_Switch",
		Row:   map[string]interface{}{"name": lswitch},
	}

	results, err := ovsdb.Transact("OVN_Northbound", insertOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating switch %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 1)
}

// ListLogicalPorts lists the logical ports in OVN.
func (ovsdb client) ListLogicalPorts() ([]LPort, error) {
	portReply, err := ovsdb.Transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition,
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing lports: %s", err)
	}

	var result []LPort
	for _, row := range portReply[0].Rows {
		result = append(result, LPort{
			uuid:      ovsUUIDFromRow(row),
			Name:      row["name"].(string),
			Addresses: ovsStringSetToSlice(row["addresses"]),
		})
	}
	return result, nil
}

// CreateLogicalPort creates a new logical port in OVN.
func (ovsdb client) CreateLogicalPort(lswitch, name, mac, ip string) error {
	addrs := newOvsSet([]string{fmt.Sprintf("%s %s", mac, ip)})

	port := map[string]interface{}{"name": name, "addresses": addrs}

	insertOp := ovs.Operation{
		Op:       "insert",
		Table:    "Logical_Switch_Port",
		Row:      port,
		UUIDName: "qlportadd",
	}

	mutateOp := ovs.Operation{
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("ports", "insert", ovs.UUID{GoUUID: "qlportadd"}),
		},
		Where: newCondition("name", "==", lswitch),
	}

	results, err := ovsdb.Transact("OVN_Northbound", insertOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating lport %s on %s: %s",
			name, lswitch, err)
	}

	return errorCheck(results, 2)
}

// DeleteLogicalPort removes a logical port from OVN.
func (ovsdb client) DeleteLogicalPort(lswitch string, lport LPort) error {
	deleteOp := ovs.Operation{
		Op:    "delete",
		Table: "Logical_Switch_Port",
		Where: newCondition("_uuid", "==", lport.uuid),
	}

	mutateOp := ovs.Operation{
		Op:        "mutate",
		Table:     "Logical_Switch",
		Mutations: []interface{}{newMutation("ports", "delete", lport.uuid)},
		Where:     newCondition("name", "==", lswitch),
	}

	results, err := ovsdb.Transact("OVN_Northbound", deleteOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting lport %s on %s: %s",
			lport.Name, lswitch, err)
	}
	return errorCheck(results, 2)
}

// ListACLs lists the access control rules in OVN.
func (ovsdb client) ListACLs() ([]ACL, error) {
	aclReply, err := ovsdb.Transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "ACL",
		Where: noCondition,
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing ACLs: %s", err)
	}

	var result []ACL
	for _, row := range aclReply[0].Rows {
		result = append(result, ACL{
			uuid: ovsUUIDFromRow(row),
			Core: ACLCore{
				Priority:  int(row["priority"].(float64)),
				Direction: row["direction"].(string),
				Match:     row["match"].(string),
				Action:    row["action"].(string),
			},
			Log: row["log"].(bool)})
	}
	return result, nil
}

// CreateACL creates an access control rule in OVN.
//
// direction, unless wildcarded, must be either "from-lport" or "to-lport"
//
// priority, unless wildcarded, must be in [1,32767]
//
// match, unless wildcarded or empty, must be a valid OpenFlow expression
//
// action must be one of {"allow", "allow-related", "drop", "reject"}
//
// direction and match may be wildcarded by passing the value "*". priority may also
// be wildcarded by passing a value less than 0.
func (ovsdb client) CreateACL(lswitch, direction string, priority int,
	match, action string) error {
	aclRow := map[string]interface{}{
		"priority": int(math.Max(0.0, float64(priority))),
		"action":   action,
		"log":      false,
	}
	if direction != "*" {
		aclRow["direction"] = direction
	}
	if match != "*" {
		aclRow["match"] = match
	}

	insertOp := ovs.Operation{
		Op:       "insert",
		Table:    "ACL",
		Row:      aclRow,
		UUIDName: "qacladd",
	}

	mutateOp := ovs.Operation{
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "insert", ovs.UUID{GoUUID: "qacladd"}),
		},
		Where: newCondition("name", "==", lswitch),
	}

	results, err := ovsdb.Transact("OVN_Northbound", insertOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating ACL on %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 2)
}

// DeleteACL removes an access control rule from OVN.
func (ovsdb client) DeleteACL(lswitch string, ovsdbACL ACL) error {
	deleteOp := ovs.Operation{
		Op:    "delete",
		Table: "ACL",
		Where: newCondition("_uuid", "==", ovsdbACL.uuid),
	}

	mutateOp := ovs.Operation{
		Op:    "mutate",
		Table: "Logical_Switch",
		Mutations: []interface{}{
			newMutation("acls", "delete", ovsdbACL.uuid),
		},
		Where: newCondition("name", "==", lswitch),
	}

	results, err := ovsdb.Transact("OVN_Northbound", deleteOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting ACL on %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 2)
}

// AddressSet is a named group of IPs in OVN.
type AddressSet struct {
	Name      string
	Addresses []string
}

// ListAddressSets lists the address sets in OVN.
func (ovsdb client) ListAddressSets() ([]AddressSet, error) {
	result := []AddressSet{}

	addressReply, err := ovsdb.Transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Address_Set",
		Where: noCondition,
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: list address sets: %s", err)
	}

	for _, addr := range addressReply[0].Rows {
		result = append(result, AddressSet{
			Name:      addr["name"].(string),
			Addresses: ovsStringSetToSlice(addr["addresses"]),
		})
	}
	return result, nil
}

// CreateAddressSet creates an address set in OVN.
func (ovsdb client) CreateAddressSet(name string, addresses []string) error {
	addrs := newOvsSet(addresses)
	addressRow := map[string]interface{}{
		"name":      name,
		"addresses": addrs,
	}

	insertOp := ovs.Operation{
		Op:    "insert",
		Table: "Address_Set",
		Row:   addressRow,
	}

	results, err := ovsdb.Transact("OVN_Northbound", insertOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating address set: %s", err)
	}
	return errorCheck(results, 1)
}

// DeleteAddressSet removes an address set from OVN.
func (ovsdb client) DeleteAddressSet(name string) error {
	deleteOp := ovs.Operation{
		Op:    "delete",
		Table: "Address_Set",
		Where: newCondition("name", "==", name),
	}

	results, err := ovsdb.Transact("OVN_Northbound", deleteOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting address set: %s", err)
	}
	return errorCheck(results, 1)
}

// OpenFlowPorts returns a map from interface name to OpenFlow port number for every
// interface in ovsdb.  Those interfaces without a port number are silently omitted.
func (ovsdb client) OpenFlowPorts() (map[string]int, error) {
	reply, err := ovsdb.Transact("Open_vSwitch", ovs.Operation{
		Op:    "select",
		Table: "Interface",
		Where: noCondition,
	})
	if err != nil {
		return nil, fmt.Errorf("select interface error: %s", err)
	}

	ifaceMap := map[string]int{}
	for _, iface := range reply[0].Rows {
		name, ok := iface["name"].(string)
		if !ok {
			continue
		}

		ofport, ok := iface["ofport"].(float64)
		if !ok {
			continue
		}

		if ofport > 0 {
			ifaceMap[name] = int(ofport)
		}
	}

	return ifaceMap, nil
}

// This does not cover all cases, they should just be added as needed
func newMutation(column, mutator string, value interface{}) mutation {
	switch typedValue := value.(type) {
	case ovs.UUID:
		uuidSlice := []ovs.UUID{typedValue}
		mutateValue := newOvsSet(uuidSlice)
		return ovs.NewMutation(column, mutator, mutateValue)
	default:
		panic(fmt.Sprintf("unhandled value in mutation: value %s, type %s",
			value, typedValue))
	}
}

func ovsStringSetToSlice(oSet interface{}) []string {
	var ret []string
	if t, ok := oSet.([]interface{}); ok && t[0] == "set" {
		for _, v := range t[1].([]interface{}) {
			ret = append(ret, v.(string))
		}
	} else {
		ret = append(ret, oSet.(string))
	}
	return ret
}

func ovsUUIDFromRow(row row) ovs.UUID {
	uuid := ovs.UUID{}
	block, ok := row["_uuid"].([]interface{})
	if !ok && len(block) != 2 {
		// This should never happen.
		panic("row does not have valid UUID")
	}
	uuid.GoUUID = block[1].(string)
	return uuid
}

func errorCheck(results []ovs.OperationResult, expectedResponses int) error {
	if len(results) < expectedResponses {
		return errors.New("mismatched responses and operations")
	}
	for i, result := range results {
		if result.Error != "" {
			return fmt.Errorf("operation %d failed due to error: %s: %s",
				i, result.Error, result.Details)
		}
	}
	return nil
}

// Get gets the element at the ith index
func (lps LPortSlice) Get(i int) interface{} {
	return lps[i]
}

// Len returns the length of the slice
func (lps LPortSlice) Len() int {
	return len(lps)
}

func newOvsSet(slice interface{}) *ovs.OvsSet {
	result, err := ovs.NewOvsSet(slice)
	if err != nil {
		panic(err)
	}
	return result
}
