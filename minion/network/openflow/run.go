package openflow

import (
	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	log "github.com/sirupsen/logrus"
)

// Run ... TODO Ethan: Document this.
func Run(conn db.Conn) {
	for range conn.TriggerTick(30, db.MinionTable, db.ContainerTable).C {
		self := conn.MinionSelf()
		if self.PrivateIP != "" {
			updateOpenflow(conn, self.PrivateIP)
		}
	}
}

func updateOpenflow(conn db.Conn, myIP string) {
	var dbcs []db.Container
	var conns []db.Connection

	txn := func(view db.Database) error {
		conns = view.SelectFromConnection(nil)
		dbcs = view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.IP != "" && dbc.Minion == myIP
		})
		return nil
	}
	conn.Txn(db.ConnectionTable, db.ContainerTable).Run(txn)

	ofcs := openflowContainers(dbcs, conns)
	if err := replaceFlows(ofcs); err != nil {
		log.WithError(err).Warning("Failed to update OpenFlow")
	}
}

func openflowContainers(dbcs []db.Container, conns []db.Connection) []Container {
	fromPubPorts := map[string][]int{}
	toPubPorts := map[string][]int{}
	for _, conn := range conns {
		for _, from := range conn.From {
			for _, to := range conn.To {
				if from != blueprint.PublicInternetLabel &&
					to != blueprint.PublicInternetLabel {
					continue
				}

				if conn.MinPort != conn.MaxPort {
					c.Inc("Unsupported Public Port Range")
					log.WithField("connection", conn).Debug(
						"Unsupported Public Port Range")
					continue
				}

				if from == blueprint.PublicInternetLabel {
					fromPubPorts[to] = append(fromPubPorts[to],
						conn.MinPort)
				}

				if to == blueprint.PublicInternetLabel {
					toPubPorts[from] = append(toPubPorts[from],
						conn.MinPort)
				}
			}
		}
	}

	var ofcs []Container
	for _, dbc := range dbcs {
		_, peerKelda := ipdef.PatchPorts(dbc.IP)

		ofc := Container{
			Veth:  ipdef.IFName(dbc.IP),
			Patch: peerKelda,
			Mac:   ipdef.IPStrToMac(dbc.IP),
			IP:    dbc.IP,

			ToPub:   map[int]struct{}{},
			FromPub: map[int]struct{}{},
		}

		for _, p := range toPubPorts[dbc.Hostname] {
			ofc.ToPub[p] = struct{}{}
		}

		for _, p := range fromPubPorts[dbc.Hostname] {
			ofc.FromPub[p] = struct{}{}
		}

		ofcs = append(ofcs, ofc)
	}
	return ofcs
}
