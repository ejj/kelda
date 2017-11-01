package network

import (
	"fmt"
	"strings"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/ovsdb"

	log "github.com/sirupsen/logrus"
)

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

func updateACLs(ovsdbClient ovsdb.Client, connections []db.Connection,
	hostnameToIP map[string]string) {
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
		if conn.From == blueprint.PublicInternetLabel ||
			conn.To == blueprint.PublicInternetLabel {
			continue
		}

		src := hostnameToIP[conn.From]
		dst := hostnameToIP[conn.To]
		if src == "" || dst == "" {
			log.WithField("connection", conn).Debug("Unknown hostname " +
				"in ACL. Ignoring")
			continue
		}

		matchStr := getMatchString(src, dst, conn.MinPort, conn.MaxPort)
		expACLs = append(expACLs, directedACLs(
			ovsdb.ACL{
				Core: ovsdb.ACLCore{
					Action:   "allow",
					Match:    matchStr,
					Priority: 1,
				},
			})...)

		icmpMatch := and(from(src), to(dst), "icmp")
		if _, ok := icmpMatches[icmpMatch]; !ok {
			icmpMatches[icmpMatch] = struct{}{}
			expACLs = append(expACLs, directedACLs(
				ovsdb.ACL{
					Core: ovsdb.ACLCore{
						Action:   "allow",
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

func getMatchString(srcIP, dstIP string, minPort, maxPort int) string {
	return or(
		and(
			and(from(srcIP), to(dstIP)),
			portConstraint(minPort, maxPort, "dst")),
		and(
			and(from(dstIP), to(srcIP)),
			portConstraint(minPort, maxPort, "src")))
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
