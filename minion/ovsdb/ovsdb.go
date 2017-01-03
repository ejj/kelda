package ovsdb

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	ovs "github.com/socketplane/libovsdb"
)

// This is simply a wrapper around all the libovsdb calls we are currently using. This
// makes it very easy to mock for testing.
type ovsdbAPI interface {
	disconnect()
	transact(db string, operation ...ovs.Operation) ([]ovs.OperationResult, error)
}

// Client is a connection to the ovsdb-server database.
type Client struct {
	ovsdbAPI
}

type ovsdbClient struct {
	*ovs.OvsdbClient
}

func (client ovsdbClient) disconnect() {
	client.Disconnect()
}

func (client ovsdbClient) transact(db string, operation ...ovs.Operation) (
	[]ovs.OperationResult, error) {
	return client.Run(db, operation...)
}

// LPort is a logical port in OVN.
type LPort struct {
	uuid      ovs.UUID
	Bridge    string
	Name      string
	Addresses []string
}

// LPortSlice is a wrapper around []LPort so it can be used in joins
type LPortSlice []LPort

// Interface is a logical interface in OVN.
type Interface struct {
	uuid        ovs.UUID
	portUUID    ovs.UUID
	Name        string
	Peer        string
	AttachedMAC string
	IfaceID     string
	Bridge      string
	Type        string
	OFPort      *int
}

const (
	// InterfaceTypePatch is the logical interface type `patch`
	InterfaceTypePatch = "patch"

	// InterfaceTypeInternal is the logical interface type `internal`
	InterfaceTypeInternal = "internal"

	// InterfaceTypeGeneve is the logical interface type `geneve`
	InterfaceTypeGeneve = "geneve"

	// InterfaceTypeSTT is the logical interface type `stt`
	InterfaceTypeSTT = "stt"
)

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
func noCondition() []interface{} {
	return []interface{}{ovs.NewCondition("_uuid", "!=", ovs.UUID{GoUUID: "_"})}
}

func newCondition(column, function string, value interface{}) []interface{} {
	return []interface{}{ovs.NewCondition(column, function, value)}
}

// Open creates a new Ovsdb connection.
// It's stored in a variable so we can mock it out for the unit tests.
var Open = func() (Client, error) {
	client, err := ovs.Connect("127.0.0.1", 6640)
	return Client{ovsdbClient{client}}, err
}

// Close destroys an Ovsdb connection created by Open.
func (ovsdb Client) Close() {
	ovsdb.disconnect()
}

// CreateLogicalSwitch creates a new logical switch in OVN.
func (ovsdb Client) CreateLogicalSwitch(lswitch string) error {
	check, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
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

	results, err := ovsdb.transact("OVN_Northbound", insertOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating switch %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 1)
}

// ListLogicalPorts lists the logical ports in OVN.
func (ovsdb Client) ListLogicalPorts(lswitch string) ([]LPort, error) {
	result := []LPort{}

	switchReply, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing switches: %s", err)
	}
	if len(switchReply[0].Rows) == 0 {
		return result, nil
	}
	logicalSwitch := switchReply[0].Rows[0]

	portReply, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch_Port",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing lports on %s: %s",
			lswitch, err)
	}
	portMap := rowUUIDMap(portReply[0].Rows)

	for _, portUUID := range ovsUUIDSetToSlice(logicalSwitch["ports"]) {
		portrow, ok := portMap[portUUID]
		if !ok {
			return nil, fmt.Errorf("missing port %v", portUUID)
		}

		result = append(result, LPort{
			uuid:      portUUID,
			Bridge:    lswitch,
			Name:      portrow["name"].(string),
			Addresses: ovsStringSetToSlice(portrow["addresses"]),
		})
	}
	return result, nil
}

// CreateLogicalPort creates a new logical port in OVN.
func (ovsdb Client) CreateLogicalPort(lswitch, name, mac, ip string) error {
	addrs, err := ovs.NewOvsSet([]string{fmt.Sprintf("%s %s", mac, ip)})
	if err != nil {
		return err
	}

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

	results, err := ovsdb.transact("OVN_Northbound", insertOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating lport %s on %s: %s",
			name, lswitch, err)
	}

	return errorCheck(results, 2)
}

// DeleteLogicalPort removes a logical port from OVN.
func (ovsdb Client) DeleteLogicalPort(lswitch string, lport LPort) error {
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

	results, err := ovsdb.transact("OVN_Northbound", deleteOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting lport %s on %s: %s",
			lport, lswitch, err)
	}
	return errorCheck(results, 2)
}

// ListACLs lists the access control rules in OVN.
func (ovsdb Client) ListACLs(lswitch string) ([]ACL, error) {
	result := []ACL{}

	switchReply, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: newCondition("name", "==", lswitch),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: retrieving lswitch %s: %s",
			lswitch, err)
	}
	if len(switchReply[0].Rows) == 0 {
		return result, nil
	}
	logicalSwitch := switchReply[0].Rows[0]

	aclReply, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "ACL",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing ACLs on %s: %s",
			lswitch, err)
	}
	aclMap := rowUUIDMap(aclReply[0].Rows)

	for _, aclUUID := range ovsUUIDSetToSlice(logicalSwitch["acls"]) {
		aclrow, ok := aclMap[aclUUID]
		if !ok {
			return nil, fmt.Errorf("missing ACL %v", aclUUID)
		}

		result = append(result, ACL{
			uuid: aclUUID,
			Core: ACLCore{
				Priority:  int(aclrow["priority"].(float64)),
				Direction: aclrow["direction"].(string),
				Match:     aclrow["match"].(string),
				Action:    aclrow["action"].(string),
			},
			Log: aclrow["log"].(bool),
		})
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
func (ovsdb Client) CreateACL(lswitch string, direction string, priority int,
	match string, action string) error {
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

	results, err := ovsdb.transact("OVN_Northbound", insertOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating ACL on %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 2)
}

// DeleteACL removes an access control rule from OVN.
func (ovsdb Client) DeleteACL(lswitch string, ovsdbACL ACL) error {
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

	results, err := ovsdb.transact("OVN_Northbound", deleteOp, mutateOp)
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
func (ovsdb Client) ListAddressSets(lswitch string) ([]AddressSet, error) {
	result := []AddressSet{}

	addressReply, err := ovsdb.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Address_Set",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: "+
			"listing address sets on %s: %s", lswitch, err)
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
func (ovsdb Client) CreateAddressSet(lswitch string, name string,
	addresses []string) error {

	addrs, err := ovs.NewOvsSet(addresses)
	if err != nil {
		return err
	}

	addressRow := map[string]interface{}{
		"name":      name,
		"addresses": addrs,
	}

	insertOp := ovs.Operation{
		Op:    "insert",
		Table: "Address_Set",
		Row:   addressRow,
	}

	results, err := ovsdb.transact("OVN_Northbound", insertOp)
	if err != nil {
		return fmt.Errorf("transaction error: creating address set on %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 1)
}

// DeleteAddressSet removes an address set from OVN.
func (ovsdb Client) DeleteAddressSet(lswitch string, name string) error {
	deleteOp := ovs.Operation{
		Op:    "delete",
		Table: "Address_Set",
		Where: newCondition("name", "==", name),
	}

	results, err := ovsdb.transact("OVN_Northbound", deleteOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting address set on %s: %s",
			lswitch, err)
	}
	return errorCheck(results, 1)
}

// ListInterfaces gets all openflow interfaces.
func (ovsdb Client) ListInterfaces() ([]Interface, error) {
	bridgeReply, err := ovsdb.transact("Open_vSwitch", ovs.Operation{
		Op:    "select",
		Table: "Bridge",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing Bridges: %s", err)
	}

	portReply, err := ovsdb.transact("Open_vSwitch", ovs.Operation{
		Op:    "select",
		Table: "Port",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing Ports: %s", err)
	}
	portMap := rowUUIDMap(portReply[0].Rows)

	ifaceReply, err := ovsdb.transact("Open_vSwitch", ovs.Operation{
		Op:    "select",
		Table: "Interface",
		Where: noCondition(),
	})
	if err != nil {
		return nil, fmt.Errorf("transaction error: listing ifaces: %s", err)
	}
	ifaceMap := rowUUIDMap(ifaceReply[0].Rows)

	result := []Interface{}
	for _, bridge := range bridgeReply[0].Rows {
		bridgeName, ok := bridge["name"].(string)
		if !ok {
			return nil, errors.New("bridge missing its name")
		}
		for _, portUUID := range ovsUUIDSetToSlice(bridge["ports"]) {
			port, ok := portMap[portUUID]
			if !ok {
				return nil, fmt.Errorf("missing port %v", portUUID)
			}

			for _, ifaceUUID := range ovsUUIDSetToSlice(port["interfaces"]) {
				iface, ok := ifaceMap[ifaceUUID]
				if !ok {
					return nil, fmt.Errorf("missing interface %v",
						ifaceUUID)
				}

				ifaceStruct, err := ifaceFromRow(iface)
				if err != nil {
					return nil, err
				}

				ifaceStruct.uuid = ifaceUUID
				ifaceStruct.portUUID = portUUID
				ifaceStruct.Bridge = bridgeName
				result = append(result, ifaceStruct)
			}
		}
	}

	return result, nil
}

// CreateInterface creates an openflow port on specified bridge.
//
// A port cannot be created without an interface, that is why the "default"
// interface (one with the same name as the port) is created along with it.
func (ovsdb Client) CreateInterface(bridge, name string) error {
	var ops []ovs.Operation

	ops = append(ops, ovs.Operation{
		Op:       "insert",
		Table:    "Interface",
		Row:      row{"name": name},
		UUIDName: "qifaceadd",
	})

	ifaces, err := ovs.NewOvsSet([]ovs.UUID{{GoUUID: "qifaceadd"}})
	if err != nil {
		return err
	}

	ops = append(ops, ovs.Operation{
		Op:       "insert",
		Table:    "Port",
		Row:      row{"name": name, "interfaces": ifaces},
		UUIDName: "qportadd",
	})

	ops = append(ops, ovs.Operation{
		Op:    "mutate",
		Table: "Bridge",
		Mutations: []interface{}{
			newMutation("ports", "insert", ovs.UUID{GoUUID: "qportadd"}),
		},
		Where: newCondition("name", "==", bridge),
	})

	results, err := ovsdb.transact("Open_vSwitch", ops...)
	if err != nil {
		return fmt.Errorf("transaction error: creating interface %s: %s",
			name, err)
	}
	return errorCheck(results, len(ops))
}

// DeleteInterface deletes an openflow interface.
func (ovsdb Client) DeleteInterface(iface Interface) error {
	deleteOp := ovs.Operation{
		Op:    "delete",
		Table: "Port",
		Where: newCondition("name", "==", iface.Name),
	}

	mutateOp := ovs.Operation{
		Op:    "mutate",
		Table: "Bridge",
		Mutations: []interface{}{
			newMutation("ports", "delete", iface.portUUID),
		},
		Where: newCondition("name", "==", iface.Bridge),
	}

	results, err := ovsdb.transact("Open_vSwitch", deleteOp, mutateOp)
	if err != nil {
		return fmt.Errorf("transaction error: deleting interface %s: %s",
			iface.Name, err)
	}
	return errorCheck(results, 2)
}

// ModifyInterface modifies the openflow interface.
func (ovsdb Client) ModifyInterface(iface Interface) error {
	var ops []ovs.Operation
	var muts []mutation

	if iface.Peer != "" {
		muts = append(muts, newMutation("options", "insert",
			map[string]string{"peer": iface.Peer}))
	}

	if iface.AttachedMAC != "" {
		muts = append(muts, newMutation("external_ids", "insert",
			map[string]string{"attached-mac": iface.AttachedMAC}))
	}

	if iface.IfaceID != "" {
		muts = append(muts, newMutation("external_ids", "insert",
			map[string]string{"iface-id": iface.IfaceID}))
	}

	if iface.Type == InterfaceTypePatch {
		ops = append(ops, ovs.Operation{
			Op:    "update",
			Table: "Interface",
			Where: newCondition("name", "==", iface.Name),
			Row:   row{"type": "patch"},
		})
	}

	for _, mut := range muts {
		ops = append(ops, ovs.Operation{
			Op:        "mutate",
			Table:     "Interface",
			Mutations: []interface{}{mut},
			Where:     newCondition("name", "==", iface.Name),
		})
	}

	if len(ops) == 0 {
		return nil
	}

	results, err := ovsdb.transact("Open_vSwitch", ops...)
	if err != nil {
		return fmt.Errorf("transaction error: modifying interface %s: %s",
			iface.Name, err)
	}

	return errorCheck(results, len(ops))
}

func ifaceFromRow(row row) (Interface, error) {
	iface := Interface{}

	name, ok := row["name"].(string)
	if !ok {
		return iface, errors.New("missing Interface key: name")
	}
	iface.Name = name

	ifaceType, ok := row["type"].(string)
	if !ok {
		return iface, errors.New("missing Interface key: type")
	}
	iface.Type = ifaceType

	optRow, ok := row["options"]
	if !ok {
		return iface, errors.New("missing Interface key: options")
	}
	options, err := ovsStringMapToMap(optRow)
	if err != nil {
		return iface, err
	}

	extRow, ok := row["external_ids"]
	if !ok {
		return iface, errors.New("missing Interface key: external_ids")
	}
	externalIDs, err := ovsStringMapToMap(extRow)
	if err != nil {
		return iface, err
	}

	ofport, ok := row["ofport"].(float64)
	if ok {
		port := int(ofport)
		iface.OFPort = &port
	}

	// The following map keys could be missing without breaking the Schema in the
	// Interface table.
	if peer, ok := options["peer"]; ok {
		iface.Peer = peer
	}

	if amac, ok := externalIDs["attached-mac"]; ok {
		iface.AttachedMAC = amac
	}

	if id, ok := externalIDs["iface-id"]; ok {
		iface.IfaceID = id
	}

	return iface, nil
}

// This does not cover all cases, they should just be added as needed
func newMutation(column, mutator string, value interface{}) mutation {
	switch typedValue := value.(type) {
	case ovs.UUID:
		uuidSlice := []ovs.UUID{typedValue}
		mutateValue, _ := ovs.NewOvsSet(uuidSlice)
		return ovs.NewMutation(column, mutator, mutateValue)
	default:
		var mutateValue interface{}
		var err error
		switch reflect.ValueOf(typedValue).Kind() {
		case reflect.Slice:
			mutateValue, err = ovs.NewOvsSet(typedValue)
		case reflect.Map:
			mutateValue, err = ovs.NewOvsMap(typedValue)
		default:
			panic(fmt.Sprintf(
				"unhandled value in mutation: value %s, type %s",
				value, typedValue))
		}
		if err != nil {
			return err
		}
		return ovs.NewMutation(column, mutator, mutateValue)
	}
}

func ovsStringMapToMap(oMap interface{}) (map[string]string, error) {
	var ret = make(map[string]string)
	wrap, ok := oMap.([]interface{})
	if !ok {
		return nil, errors.New("ovs map outermost layer invalid")
	}
	if wrap[0] != "map" {
		return nil, errors.New("ovs map invalid identifier")
	}

	brokenMap, ok := wrap[1].([]interface{})
	if !ok {
		return nil, errors.New("ovs map content invalid")
	}
	for _, kvPair := range brokenMap {
		kvSlice, ok := kvPair.([]interface{})
		if !ok {
			return nil, errors.New("ovs map block must be a slice")
		}
		key, ok := kvSlice[0].(string)
		if !ok {
			return nil, errors.New("ovs map key must be string")
		}
		val, ok := kvSlice[1].(string)
		if !ok {
			return nil, errors.New("ovs map value must be string")
		}
		ret[key] = val
	}
	return ret, nil
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

func ovsUUIDSetToSlice(oSet interface{}) []ovs.UUID {
	var ret []ovs.UUID
	if t, ok := oSet.([]interface{}); ok && t[0] == "set" {
		for _, v := range t[1].([]interface{}) {
			ret = append(ret, ovs.UUID{
				GoUUID: v.([]interface{})[1].(string),
			})
		}
	} else {
		ret = append(ret, ovs.UUID{
			GoUUID: oSet.([]interface{})[1].(string),
		})
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

func rowUUIDMap(rows []map[string]interface{}) map[ovs.UUID]row {
	res := map[ovs.UUID]row{}
	for _, row := range rows {
		uuid := ovsUUIDFromRow(row)
		res[uuid] = row
	}
	return res
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

// InterfaceSlice is used for HashJoin.
type InterfaceSlice []Interface

// Get is required for HashJoin.
func (ovsps InterfaceSlice) Get(i int) interface{} {
	return ovsps[i]
}

// Len is required for HashJoin.
func (ovsps InterfaceSlice) Len() int {
	return len(ovsps)
}

// Get gets the element at the ith index
func (lps LPortSlice) Get(i int) interface{} {
	return lps[i]
}

// Len returns the length of the slice
func (lps LPortSlice) Len() int {
	return len(lps)
}
