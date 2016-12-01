package ovsdb

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/NetSys/quilt/join"
	ovs "github.com/socketplane/libovsdb"
	"github.com/stretchr/testify/assert"
)

func TestCreateLogicalSwitch(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	// Create new switch.
	lswitch := "test-switch"
	err := ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.Nil(t, err)

	// Check existence of created switch.
	switchReply, err := ovsdbClient.transact("OVN_Northbound", ovs.Operation{
		Op:    "select",
		Table: "Logical_Switch",
		Where: newCondition("name", "==", lswitch),
	})
	assert.Nil(t, err)
	assert.Equal(t, 1, len(switchReply[0].Rows))

	// Try to create the same switch. Should now return error since it exists.
	err = ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.NotNil(t, err)
}

func TestLogicalPorts(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	scoreFun := func(left, right interface{}) int {
		ovsdbPort := left.(LPort)
		localPort := right.(LPort)

		switch {
		case ovsdbPort.Name != localPort.Name:
			return -1
		case !reflect.DeepEqual(ovsdbPort.Addresses,
			localPort.Addresses):
			return -1
		default:
			return 0
		}
	}

	checkCorrectness := func(ovsdbLPorts []LPort, localLPorts ...LPort) {
		pair, _, _ := join.Join(ovsdbLPorts, localLPorts, scoreFun)
		assert.Equal(t, len(pair), len(localLPorts))
	}

	// Create new switch.
	lswitch := "test-switch"
	err := ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.Nil(t, err)

	// Nothing happens yet. It should have zero logical port to be listed.
	ovsdbLPorts, err := ovsdbClient.ListLogicalPorts(lswitch)
	assert.Nil(t, err)
	assert.Zero(t, len(ovsdbLPorts))

	// Create logical port.
	name1, mac1, ip1 := "lp1", "00:00:00:00:00:00", "0.0.0.0"
	lport1 := LPort{
		Name:      "lp1",
		Addresses: []string{fmt.Sprintf("%s %s", mac1, ip1)},
	}
	err = ovsdbClient.CreateLogicalPort(lswitch, name1, mac1, ip1)
	assert.Nil(t, err)

	// It should now have one logical port to be listed.
	ovsdbLPorts, err = ovsdbClient.ListLogicalPorts(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ovsdbLPorts))

	ovsdbLPort1 := ovsdbLPorts[0]

	checkCorrectness(ovsdbLPorts, lport1)

	// Create a second logical port.
	name2, mac2, ip2 := "lp2", "00:00:00:00:00:01", "0.0.0.1"
	lport2 := LPort{
		Name:      "lp2",
		Addresses: []string{fmt.Sprintf("%s %s", mac2, ip2)},
	}
	err = ovsdbClient.CreateLogicalPort(lswitch, name2, mac2, ip2)
	assert.Nil(t, err)

	// It should now have two logical ports to be listed.
	ovsdbLPorts, err = ovsdbClient.ListLogicalPorts(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ovsdbLPorts))

	checkCorrectness(ovsdbLPorts, lport1, lport2)

	// Delete the first logical port.
	err = ovsdbClient.DeleteLogicalPort(lswitch, ovsdbLPort1)
	assert.Nil(t, err)

	// It should now have one logical port to be listed.
	ovsdbLPorts, err = ovsdbClient.ListLogicalPorts(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ovsdbLPorts))

	ovsdbLPort2 := ovsdbLPorts[0]

	checkCorrectness(ovsdbLPorts, lport2)

	// Delete the last one as well.
	err = ovsdbClient.DeleteLogicalPort(lswitch, ovsdbLPort2)
	assert.Nil(t, err)

	// It should now have one logical port to be listed.
	ovsdbLPorts, err = ovsdbClient.ListLogicalPorts(lswitch)
	assert.Nil(t, err)
	assert.Zero(t, len(ovsdbLPorts))
}

func TestACLs(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	key := func(val interface{}) interface{} {
		return val.(ACL).Core
	}

	checkCorrectness := func(ovsdbACLs []ACL, localACLs ...ACL) {
		pair, _, _ := join.HashJoin(ACLSlice(ovsdbACLs), ACLSlice(localACLs),
			key, key)
		assert.Equal(t, len(pair), len(localACLs))
	}

	// Create new switch.
	lswitch := "test-switch"
	err := ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.Nil(t, err)

	// Create one ACL rule.
	localCore1 := ACLCore{
		Priority:  1,
		Direction: "from-lport",
		Match:     "0.0.0.0",
		Action:    "allow",
	}

	localACL1 := ACL{
		Core: localCore1,
		Log:  false,
	}

	err = ovsdbClient.CreateACL(lswitch, localCore1.Direction, localCore1.Priority,
		localCore1.Match, localCore1.Action)
	assert.Nil(t, err)

	// It should now have one ACL entry to be listed.
	ovsdbACLs, err := ovsdbClient.ListACLs(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ovsdbACLs))

	ovsdbACL1 := ovsdbACLs[0]

	checkCorrectness(ovsdbACLs, localACL1)

	// Create one more ACL rule.
	localCore2 := ACLCore{
		Priority:  2,
		Direction: "from-lport",
		Match:     "0.0.0.1",
		Action:    "drop",
	}
	localACL2 := ACL{
		Core: localCore2,
		Log:  false,
	}

	err = ovsdbClient.CreateACL(lswitch, localCore2.Direction, localCore2.Priority,
		localCore2.Match, localCore2.Action)
	assert.Nil(t, err)

	// It should now have two ACL entries to be listed.
	ovsdbACLs, err = ovsdbClient.ListACLs(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ovsdbACLs))

	checkCorrectness(ovsdbACLs, localACL1, localACL2)

	// Delete the first ACL rule.
	err = ovsdbClient.DeleteACL(lswitch, ovsdbACL1)
	assert.Nil(t, err)

	// It should now have only one ACL entry to be listed.
	ovsdbACLs, err = ovsdbClient.ListACLs(lswitch)
	assert.Nil(t, err)

	ovsdbACL2 := ovsdbACLs[0]

	assert.Equal(t, 1, len(ovsdbACLs))

	checkCorrectness(ovsdbACLs, localACL2)

	// Delete the other ACL rule.
	err = ovsdbClient.DeleteACL(lswitch, ovsdbACL2)
	assert.Nil(t, err)

	// It should now have only one ACL entry to be listed.
	ovsdbACLs, err = ovsdbClient.ListACLs(lswitch)
	assert.Nil(t, err)
	assert.Zero(t, len(ovsdbACLs))
}

// We can't use a slice in the HashJoin key, so we represent the addresses in
// an address set as the addresses concatenated together.
type addressSetKey struct {
	name      string
	addresses string
}

func newAddressSetKey(name string, addresses []string) addressSetKey {
	// OVSDB returns the addresses in a non-deterministic order, so we
	// sort them.
	sort.Strings(addresses)
	return addressSetKey{
		name:      name,
		addresses: strings.Join(addresses, " "),
	}
}

func TestAddressSets(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	key := func(intf interface{}) interface{} {
		addrSet := intf.(AddressSet)
		// OVSDB returns the addresses in a non-deterministic order, so we
		// sort them.
		sort.Strings(addrSet.Addresses)
		return addressSetKey{
			name:      addrSet.Name,
			addresses: strings.Join(addrSet.Addresses, " "),
		}
	}

	checkCorrectness := func(ovsdbAddrSets []AddressSet, expAddrSets ...AddressSet) {
		pair, _, _ := join.HashJoin(addressSlice(ovsdbAddrSets),
			addressSlice(expAddrSets), key, key)
		assert.Equal(t, len(pair), len(expAddrSets))
	}

	// Create new switch.
	lswitch := "test-switch"
	err := ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.Nil(t, err)

	// Create one Address Set.
	addrSet1 := AddressSet{
		Name:      "red",
		Addresses: []string{"foo", "bar"},
	}

	err = ovsdbClient.CreateAddressSet(lswitch, addrSet1.Name, addrSet1.Addresses)
	assert.Nil(t, err)

	// It should now have one ACL entry to be listed.
	ovsdbAddrSets, err := ovsdbClient.ListAddressSets(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ovsdbAddrSets))

	checkCorrectness(ovsdbAddrSets, addrSet1)

	// Create one more address set.
	addrSet2 := AddressSet{
		Name:      "blue",
		Addresses: []string{"bar", "baz"},
	}

	err = ovsdbClient.CreateAddressSet(lswitch, addrSet2.Name, addrSet2.Addresses)
	assert.Nil(t, err)

	// It should now have two address sets to be listed.
	ovsdbAddrSets, err = ovsdbClient.ListAddressSets(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ovsdbAddrSets))

	checkCorrectness(ovsdbAddrSets, addrSet1, addrSet2)

	// Delete the first address set.
	err = ovsdbClient.DeleteAddressSet(lswitch, addrSet1.Name)
	assert.Nil(t, err)

	// It should now have only one address set to be listed.
	ovsdbAddrSets, err = ovsdbClient.ListAddressSets(lswitch)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ovsdbAddrSets))

	checkCorrectness(ovsdbAddrSets, addrSet2)

	// Delete the other ACL rule.
	err = ovsdbClient.DeleteAddressSet(lswitch, addrSet2.Name)
	assert.Nil(t, err)

	// It should now have only one address set to be listed.
	ovsdbAddrSets, err = ovsdbClient.ListAddressSets(lswitch)
	assert.Nil(t, err)
	assert.Zero(t, len(ovsdbAddrSets))
}

func TestInterfaces(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	// Create new switch.
	lswitch1 := "test-switch-1"
	err := ovsdbClient.CreateLogicalSwitch(lswitch1)
	assert.Nil(t, err)

	key := func(val interface{}) interface{} {
		iface := val.(Interface)
		return struct {
			name, bridge string
			ofport       int
		}{
			name:   iface.Name,
			bridge: iface.Bridge,
			ofport: *iface.OFPort,
		}
	}

	checkCorrectness := func(ovsdbIfaces []Interface, localIfaces ...Interface) {
		pair, _, _ := join.HashJoin(InterfaceSlice(ovsdbIfaces),
			InterfaceSlice(localIfaces), key, key)
		assert.Equal(t, len(pair), len(ovsdbIfaces))
	}

	// Create a new Bridge. In quilt this is usually done in supervisor.
	_, err = ovsdbClient.transact("Open_vSwitch", ovs.Operation{
		Op:    "insert",
		Table: "Bridge",
		Row:   map[string]interface{}{"name": lswitch1},
	})
	assert.Nil(t, err)

	// Ovsdb mock uses defaultOFPort as the ofport created for each interface.
	expectedOFPort := int(defaultOFPort)

	// Create one interface.
	iface1 := Interface{
		Name:   "iface1",
		Bridge: lswitch1,
		OFPort: &expectedOFPort,
	}

	err = ovsdbClient.CreateInterface(iface1.Bridge, iface1.Name)
	assert.Nil(t, err)

	ifaces, err := ovsdbClient.ListInterfaces()
	assert.Nil(t, err)
	assert.Equal(t, 1, len(ifaces))

	checkCorrectness(ifaces, iface1)

	// Now create a new switch and bridge. Attach one new interface to them.

	// Create new switch.
	lswitch2 := "test-switch-2"
	err = ovsdbClient.CreateLogicalSwitch(lswitch2)
	assert.Nil(t, err)

	// Create a new Bridge.
	_, err = ovsdbClient.transact("Open_vSwitch", ovs.Operation{
		Op:    "insert",
		Table: "Bridge",
		Row:   map[string]interface{}{"name": lswitch2},
	})
	assert.Nil(t, err)

	// Create a new interface.
	iface2 := Interface{
		Name:   "iface2",
		Bridge: lswitch2,
		OFPort: &expectedOFPort,
	}

	err = ovsdbClient.CreateInterface(iface2.Bridge, iface2.Name)
	assert.Nil(t, err)

	ifaces, err = ovsdbClient.ListInterfaces()
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ifaces))

	checkCorrectness(ifaces, iface1, iface2)

	iface1, iface2 = ifaces[0], ifaces[1]

	// Delete interface 1.
	err = ovsdbClient.DeleteInterface(iface1)
	assert.Nil(t, err)

	ifaces, err = ovsdbClient.ListInterfaces()
	assert.Nil(t, err)

	assert.Equal(t, 1, len(ifaces))

	checkCorrectness(ifaces, iface2)

	// Delete interface 2.
	err = ovsdbClient.DeleteInterface(iface2)
	assert.Nil(t, err)

	ifaces, err = ovsdbClient.ListInterfaces()
	assert.Nil(t, err)
	assert.Zero(t, len(ifaces))

	// Test ModifyInterface. We do this by creating an interface with type peer,
	// attach a mac address to it, and add external_ids.
	iface := Interface{
		Name:        "test-modify-iface",
		Peer:        "lolz",
		AttachedMAC: "00:00:00:00:00:00",
		Bridge:      lswitch1,
		Type:        "patch",
	}

	err = ovsdbClient.CreateInterface(iface.Bridge, iface.Name)
	assert.Nil(t, err)

	err = ovsdbClient.ModifyInterface(iface)
	assert.Nil(t, err)

	ifaces, err = ovsdbClient.ListInterfaces()
	assert.Nil(t, err)

	ovsdbIface := ifaces[0]
	iface.uuid = ovsdbIface.uuid
	iface.portUUID = ovsdbIface.portUUID
	iface.OFPort = ovsdbIface.OFPort
	assert.Equal(t, iface, ovsdbIface)
}

func TestBridgeMac(t *testing.T) {
	ovsdbClient := NewFakeOvsdbClient()

	// Create new switch and a new bridge.
	lswitch := "test-switch"
	err := ovsdbClient.CreateLogicalSwitch(lswitch)
	assert.Nil(t, err)

	_, err = ovsdbClient.transact("Open_vSwitch", ovs.Operation{
		Op:    "insert",
		Table: "Bridge",
		Row:   map[string]interface{}{"name": lswitch},
	})
	assert.Nil(t, err)

	mac := "00:00:00:00:00:00"
	err = ovsdbClient.SetBridgeMac(lswitch, mac)
	assert.Nil(t, err)

	bridgeReply, err := ovsdbClient.transact("Open_vSwitch", ovs.Operation{
		Op:    "select",
		Table: "Bridge",
		Where: noCondition(),
	})
	assert.Nil(t, err)

	bridge := bridgeReply[0].Rows[0]
	otherConfig := bridge["other_config"].([]interface{})[1].(map[string]interface{})
	assert.Equal(t, mac, otherConfig["hwaddr"])
}

type ACLSlice []ACL

func (acls ACLSlice) Get(i int) interface{} {
	return acls[i]
}

func (acls ACLSlice) Len() int {
	return len(acls)
}

type addressSlice []AddressSet

func (slc addressSlice) Len() int {
	return len(slc)
}

func (slc addressSlice) Get(i int) interface{} {
	return slc[i]
}
