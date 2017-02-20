package network

import (
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"sort"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/minion/ipdef"

	log "github.com/Sirupsen/logrus"
)

func runUpdateIPs(conn db.Conn) {
	for range conn.Trigger(db.ContainerTable, db.LabelTable, db.EtcdTable).C {
		if !conn.EtcdLeader() {
			continue
		}

		txn := conn.Txn(db.ContainerTable, db.LabelTable)
		err := txn.Run(func(view db.Database) error {
			err := allocateContainerIPs(view)
			if err == nil {
				err = updateLabelIPs(view)
			}
			return err
		})

		if err != nil {
			log.WithError(err).Warn("Failed to allocate IP addresses")
		}
	}
}

func allocateContainerIPs(view db.Database) error {
	dbcs := view.SelectFromContainer(nil)

	ipSet := map[string]struct{}{
		ipdef.GatewayIP.String(): {},

		// While not strictly required, it would be odd to allocate 10.0.0.0.
		ipdef.QuiltSubnet.IP.String(): {},
	}

	var unassigned []db.Container
	for _, dbc := range dbcs {
		if dbc.IP != "" {
			ipSet[dbc.IP] = struct{}{}
		} else {
			unassigned = append(unassigned, dbc)
		}
	}

	for _, dbc := range unassigned {
		ip, err := allocateIP(ipSet, ipdef.QuiltSubnet)
		if err != nil {
			return err
		}

		dbc.IP = ip
		view.Commit(dbc)
	}

	return nil
}

func updateLabelIPs(view db.Database) error {
	dbcs := view.SelectFromContainer(func(dbc db.Container) bool {
		return dbc.IP != ""
	})

	// XXX:  We sort the containers by StitchID to guarantee that the sub-label
	// ordering is consistent between function calls.  This is pretty darn fragile.
	sort.Sort(db.ContainerSlice(dbcs))

	containerIPs := map[string][]string{}
	for _, dbc := range dbcs {
		for _, l := range dbc.Labels {
			containerIPs[l] = append(containerIPs[l], dbc.IP)
		}
	}

	labelKeyFunc := func(val interface{}) interface{} {
		return val.(db.Label).Label
	}

	labelKeySlice := join.StringSlice{}
	for l := range containerIPs {
		labelKeySlice = append(labelKeySlice, l)
	}

	labels := db.LabelSlice(view.SelectFromLabel(nil))
	pairs, dbls, dbcLabels := join.HashJoin(labels, labelKeySlice, labelKeyFunc, nil)

	for _, dbl := range dbls {
		view.Remove(dbl.(db.Label))
	}

	for _, label := range dbcLabels {
		pairs = append(pairs, join.Pair{L: view.InsertLabel(), R: label})
	}

	for _, pair := range pairs {
		dbl := pair.L.(db.Label)
		dbl.Label = pair.R.(string)
		dbl.ContainerIPs = containerIPs[dbl.Label]

		// XXX: In effect, we're implementing a dumb load balancer where all
		// traffic goes to the first container.  Something more sophisticated is
		// coming (hopefully).
		dbl.IP = ""
		if len(dbl.ContainerIPs) > 0 {
			dbl.IP = dbl.ContainerIPs[0]
		}
		view.Commit(dbl)
	}

	return nil
}

func allocateIP(ipSet map[string]struct{}, subnet net.IPNet) (string, error) {
	prefix := binary.BigEndian.Uint32(subnet.IP.To4())
	mask := binary.BigEndian.Uint32(subnet.Mask)

	randStart := rand32() & ^mask
	for offset := uint32(0); offset <= ^mask; offset++ {

		randIP32 := ((randStart + offset) & ^mask) | (prefix & mask)

		randIP := net.IP(make([]byte, 4))
		binary.BigEndian.PutUint32(randIP, randIP32)
		randIPStr := randIP.String()

		if _, ok := ipSet[randIPStr]; !ok {
			ipSet[randIPStr] = struct{}{}
			return randIPStr, nil
		}
	}
	return "", errors.New("IP pool exhausted")
}

var rand32 = rand.Uint32
