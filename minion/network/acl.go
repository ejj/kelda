package network

import (
	"fmt"
	"sort"
	"strings"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/minion/ovsdb"
	"github.com/quilt/quilt/stitch"

	log "github.com/Sirupsen/logrus"
)

func updateACLs(client ovsdb.Client, connections []db.Connection, labels []db.Label) {
	syncAddressSets(client, labels)
	syncACLs(client, connections)
}

// We can't use a slice in the HashJoin key, so we represent the addresses in
// an address set as the addresses concatenated together.
type addressSetKey struct {
	name      string
	addresses string
}

// unique returns the unique elemnts of `lst`.
func unique(lst []string) (uniq []string) {
	set := make(map[string]struct{})
	for _, elem := range lst {
		set[elem] = struct{}{}
	}

	for elem := range set {
		uniq = append(uniq, elem)
	}
	return uniq
}

func syncAddressSets(ovsdbClient ovsdb.Client, labels []db.Label) {
	ovsdbAddresses, err := ovsdbClient.ListAddressSets()
	if err != nil {
		log.WithError(err).Error("Failed to list address sets")
		return
	}

	var expAddressSets []ovsdb.AddressSet
	for _, l := range labels {
		if l.Label == stitch.PublicInternetLabel {
			continue
		}
		expAddressSets = append(expAddressSets,
			ovsdb.AddressSet{
				Name:      addressSetName(l.Label),
				Addresses: unique(append(l.ContainerIPs, l.IP)),
			},
		)
	}
	ovsdbKey := func(intf interface{}) interface{} {
		addrSet := intf.(ovsdb.AddressSet)
		// OVSDB returns the addresses in a non-deterministic order, so we
		// sort them.
		sort.Strings(addrSet.Addresses)
		return addressSetKey{
			name:      addrSet.Name,
			addresses: strings.Join(addrSet.Addresses, " "),
		}
	}
	_, toCreate, toDelete := join.HashJoin(addressSlice(expAddressSets),
		addressSlice(ovsdbAddresses), ovsdbKey, ovsdbKey)

	for _, intf := range toDelete {
		addr := intf.(ovsdb.AddressSet)
		if err := ovsdbClient.DeleteAddressSet(addr.Name); err != nil {
			log.WithError(err).Warn("Error deleting address set")
		}
	}

	for _, intf := range toCreate {
		addr := intf.(ovsdb.AddressSet)
		if err := ovsdbClient.CreateAddressSet(
			addr.Name, addr.Addresses); err != nil {
			log.WithError(err).Warn("Error adding address set")
		}
	}
}

type aclKey struct {
	drop  bool
	match string
}

func directedACLs(acl ovsdb.ACL) (res []ovsdb.ACL) {
	for _, dir := range []string{"from-lport", "to-lport"} {
		res = append(res, ovsdb.ACL{
			Core: ovsdb.ACLCore{
				Direction: dir,
				Action:    acl.Core.Action,
				Match:     acl.Core.Match,
				Priority:  acl.Core.Priority,
			},
		})
	}
	return res
}

func syncACLs(ovsdbClient ovsdb.Client, connections []db.Connection) {
	ovsdbACLs, err := ovsdbClient.ListACLs()
	if err != nil {
		log.WithError(err).Error("Failed to list ACLs")
		return
	}

	expACLs := directedACLs(ovsdb.ACL{
		Core: ovsdb.ACLCore{
			Action:   "drop",
			Match:    "ip",
			Priority: 0,
		},
	})

	for _, conn := range connections {
		if conn.From == stitch.PublicInternetLabel ||
			conn.To == stitch.PublicInternetLabel {
			continue
		}
		expACLs = append(expACLs, directedACLs(
			ovsdb.ACL{
				Core: ovsdb.ACLCore{
					Action:   "allow",
					Match:    matchString(conn),
					Priority: 1,
				},
			})...)
	}

	ovsdbKey := func(ovsdbIntf interface{}) interface{} {
		return ovsdbIntf.(ovsdb.ACL).Core
	}
	_, toCreate, toDelete := join.HashJoin(ovsdbACLSlice(expACLs),
		ovsdbACLSlice(ovsdbACLs), ovsdbKey, ovsdbKey)

	for _, acl := range toDelete {
		if err := ovsdbClient.DeleteACL(lSwitch, acl.(ovsdb.ACL)); err != nil {
			log.WithError(err).Warn("Error deleting ACL")
		}
	}

	for _, intf := range toCreate {
		acl := intf.(ovsdb.ACL).Core
		if err := ovsdbClient.CreateACL(lSwitch, acl.Direction,
			acl.Priority, acl.Match, acl.Action); err != nil {
			log.WithError(err).Warn("Error adding ACL")
		}
	}
}

func matchString(c db.Connection) string {
	return or(
		and(
			and(from(c.From), to(c.To)),
			portConstraint(c.MinPort, c.MaxPort, "dst")),
		and(
			and(from(c.To), to(c.From)),
			portConstraint(c.MinPort, c.MaxPort, "src")))
}

func portConstraint(minPort, maxPort int, direction string) string {
	return fmt.Sprintf("(icmp || %[1]d <= udp.%[2]s <= %[3]d || "+
		"%[1]d <= tcp.%[2]s <= %[3]d)", minPort, direction, maxPort)
}

func from(label string) string {
	return fmt.Sprintf("ip4.src == $%s", addressSetName(label))
}

func to(label string) string {
	return fmt.Sprintf("ip4.dst == $%s", addressSetName(label))
}

func or(predicates ...string) string {
	return "(" + strings.Join(predicates, " || ") + ")"
}

func and(predicates ...string) string {
	return "(" + strings.Join(predicates, " && ") + ")"
}

// addressSetName converts `label` to a valid OVS address set name.
// It only handles the case where the label contains a hyphen. It does so by
// replacing the hyphen with an underscore, and upper-casing the entire string.
// Because labels are guaranteed to be lowercase by the language, the resulting
// label is guaranteed to not conflict with any other labels.
func addressSetName(label string) string {
	if strings.Contains(label, "-") {
		return strings.ToUpper(strings.Replace(label, "-", "_", -1))
	}
	return label
}

// ovsdbACLSlice is a wrapper around []ovsdb.ACL to allow us to perform a join
type ovsdbACLSlice []ovsdb.ACL

// Len returns the length of the slice
func (slc ovsdbACLSlice) Len() int {
	return len(slc)
}

// Get returns the element at index i of the slice
func (slc ovsdbACLSlice) Get(i int) interface{} {
	return slc[i]
}

// addressSlice is a wrapper around []ovsdb.AddressSet to allow us to perform a join
type addressSlice []ovsdb.AddressSet

// Len returns the length of the slice
func (slc addressSlice) Len() int {
	return len(slc)
}

// Get returns the element at index i of the slice
func (slc addressSlice) Get(i int) interface{} {
	return slc[i]
}
