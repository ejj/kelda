package network

import (
	"fmt"
	"sort"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/util/str"
)

var subnetC = counter.New("Subnet Sync")

// WriteSubnets syncs all IPv4 subnets governed by routes on the machine's
// network stack into the minion table.
func WriteSubnets(conn db.Conn) {
	writeSubnetsOnce(conn)
	for range time.Tick(30 * time.Second) {
		if err := writeSubnetsOnce(conn); err != nil {
			log.WithError(err).Error("Failed to sync subnets")
		}
	}
}

func writeSubnetsOnce(conn db.Conn) error {
	routes, err := nl.N.RouteList(syscall.AF_INET)
	if err != nil {
		return fmt.Errorf("list routes: %s", err)
	}

	var subnets []string
	for _, r := range routes {
		link, err := nl.N.LinkByIndex(r.LinkIndex)
		if err != nil {
			return fmt.Errorf("get link: %s", err)
		}

		// Ignore the OVN interface and the default route.
		if link.Attrs().Name == ipdef.KeldaBridge || r.Dst == nil {
			continue
		}
		subnets = append(subnets, r.Dst.String())
	}
	sort.Strings(subnets)

	conn.Txn(db.MinionTable).Run(func(view db.Database) error {
		self := view.MinionSelf()
		if !str.SliceEq(subnets, self.HostSubnets) {
			subnetC.Inc("Update subnets")
			self.HostSubnets = subnets
			view.Commit(self)
		}
		return nil
	})
	return nil
}
