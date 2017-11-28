package network

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
)

type aclKey struct {
	drop  bool
	match string
}

type connection struct {
	// These contain either address set names, or an individual IP address.
	from string
	to   string

	minPort int
	maxPort int
}

func updateACLs(client ovsdb.Client, dbConns []db.Connection,
	hostnameToIP map[string]string) {

	connections, addressSets := resolveConnections(dbConns, hostnameToIP)
	syncAddressSets(client, addressSets)
	syncACLs(client, connections)
}

func resolveConnections(dbConns []db.Connection, hostnameToIP map[string]string) (
	[]connection, []ovsdb.AddressSet) {

	var conns []connection
	addressSets := map[string][]string{}

	// Given a slice of db.Connection, create a slice of create a slice of connection
	// structs where the from and to are replaced either by individual IP addresses,
	// or the name of an address set that contains a list of IP addresses.
	for _, dbConn := range dbConns {
		from := str.SliceFilterOut(dbConn.From, blueprint.PublicInternetLabel)
		from = resolveHostnames(from, hostnameToIP)

		to := str.SliceFilterOut(dbConn.To, blueprint.PublicInternetLabel)
		to = resolveHostnames(to, hostnameToIP)

		if len(from) == 0 || len(to) == 0 {
			continue // Either from or to contained only `public`.
		}

		conns = append(conns, connection{
			minPort: dbConn.MinPort,
			maxPort: dbConn.MaxPort,
			from:    endpointName(from, addressSets),
			to:      endpointName(to, addressSets),
		})
	}

	var result []ovsdb.AddressSet
	for name, addresses := range addressSets {
		result = append(result, ovsdb.AddressSet{
			Name:      name,
			Addresses: addresses,
		})
	}

	return conns, result
}

func resolveHostnames(hostnames []string, hostnameToIP map[string]string) []string {
	var res []string
	for _, m := range hostnames {
		ip, ok := hostnameToIP[m]
		if !ok {
			log.WithField("hostname", m).Debug("Unknown hostname in ACL")
			continue
		}
		res = append(res, ip)
	}
	return res
}

func syncAddressSets(ovsdbClient ovsdb.Client, expSets []ovsdb.AddressSet) {
	sets, err := ovsdbClient.ListAddressSets()
	if err != nil {
		log.WithError(err).Error("Failed to list address sets")
		return
	}

	ovsdbKey := func(intf interface{}) interface{} {
		set := intf.(ovsdb.AddressSet)

		// OVSDB returns addresses in a non-deterministic order so we sort them.
		sort.Strings(set.Addresses)
		return strings.Join(append(set.Addresses, set.Name), "")
	}

	_, toCreateI, toDeleteI := join.HashJoin(ovsdbAddressSetSlice(expSets),
		ovsdbAddressSetSlice(sets), ovsdbKey, ovsdbKey)

	var toDelete []ovsdb.AddressSet
	for _, set := range toDeleteI {
		toDelete = append(toDelete, set.(ovsdb.AddressSet))
	}

	var toCreate []ovsdb.AddressSet
	for _, set := range toCreateI {
		toCreate = append(toCreate, set.(ovsdb.AddressSet))
	}

	if err := ovsdbClient.DeleteAddressSets(toDelete); err != nil {
		log.WithError(err).Warn("Error deleting address set")
	}

	if err := ovsdbClient.CreateAddressSets(toCreate); err != nil {
		log.WithError(err).Warn("Error adding address set")
	}
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

func syncACLs(ovsdbClient ovsdb.Client, connections []connection) {
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

	icmpMatches := map[string]struct{}{}
	for _, conn := range connections {
		matchStr := getMatchString(conn)
		expACLs = append(expACLs, directedACLs(
			ovsdb.ACL{
				Core: ovsdb.ACLCore{
					Action:   "allow",
					Match:    matchStr,
					Priority: 1,
				},
			})...)

		icmpMatch := and(from(conn.from), to(conn.to), "icmp")
		if _, ok := icmpMatches[icmpMatch]; !ok {
			icmpMatches[icmpMatch] = struct{}{}
			expACLs = append(expACLs, directedACLs(
				// Although the TCP and UDP rules use the "allow"
				// action, we use "allow-related" for ICMP since
				// the performance impact is not as large, and
				// there is no way to restrict ICMP return traffic
				// based on ports.
				ovsdb.ACL{
					Core: ovsdb.ACLCore{
						Action:   "allow-related",
						Match:    icmpMatch,
						Priority: 1,
					},
				})...)
		}
	}

	ovsdbKey := func(ovsdbIntf interface{}) interface{} {
		return ovsdbIntf.(ovsdb.ACL).Core
	}
	_, toCreateI, toDeleteI := join.HashJoin(ovsdbACLSlice(expACLs),
		ovsdbACLSlice(ovsdbACLs), ovsdbKey, ovsdbKey)

	var toDelete []ovsdb.ACL
	for _, acl := range toDeleteI {
		toDelete = append(toDelete, acl.(ovsdb.ACL))
	}

	var toCreate []ovsdb.ACLCore
	for _, acl := range toCreateI {
		toCreate = append(toCreate, acl.(ovsdb.ACL).Core)
	}

	if err := ovsdbClient.DeleteACLs(lSwitch, toDelete); err != nil {
		log.WithError(err).Warn("Error deleting ACL")
	}

	if err := ovsdbClient.CreateACLs(lSwitch, toCreate); err != nil {
		log.WithError(err).Warn("Error adding ACLs")
	}
}

func getMatchString(conn connection) string {
	// Allow From to talk to To with the appropriate dest port, and allow the
	// response traffic generated by To to reach From with the appropriate
	// corresponding source port.
	return or(
		and(
			from(conn.from), to(conn.to),
			portConstraint(conn.minPort, conn.maxPort, "dst")),
		and(
			from(conn.to), to(conn.from),
			portConstraint(conn.minPort, conn.maxPort, "src")))
}

func portConstraint(minPort, maxPort int, direction string) string {
	return fmt.Sprintf("(%[1]d <= udp.%[2]s <= %[3]d || "+
		"%[1]d <= tcp.%[2]s <= %[3]d)", minPort, direction, maxPort)
}

func from(ip string) string {
	return fmt.Sprintf("ip4.src == %s", ip)
}

func to(ip string) string {
	return fmt.Sprintf("ip4.dst == %s", ip)
}

func or(predicates ...string) string {
	return "(" + strings.Join(predicates, " || ") + ")"
}

func and(predicates ...string) string {
	return "(" + strings.Join(predicates, " && ") + ")"
}

// Creates an address set for `members` and returns the name of the address set in a
// format suitable for the connection struct.
func endpointName(members []string, addressSets map[string][]string) string {
	if len(members) == 1 {
		return members[0]
	}

	sort.Strings(members) // The hash shouldn't depend on order.
	// OVN requires address set names to start with a letter, hence the "sha" prefix.
	name := fmt.Sprintf("sha%x", sha256.Sum256([]byte(strings.Join(members, ""))))

	if _, ok := addressSets[name]; !ok {
		addressSets[name] = members
	}
	return "$" + name
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

type ovsdbAddressSetSlice []ovsdb.AddressSet

// Len returns the length of the slice
func (slc ovsdbAddressSetSlice) Len() int {
	return len(slc)
}

// Get returns the element at index i of the slice
func (slc ovsdbAddressSetSlice) Get(i int) interface{} {
	return slc[i]
}
