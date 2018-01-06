package scheduler

import (
	"errors"
	"sync"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/vault"

	log "github.com/sirupsen/logrus"
)

const labelKey = "kelda"
const labelValue = "scheduler"
const labelPair = labelKey + "=" + labelValue
const filesKey = "files"
const concurrencyLimit = 32

var once sync.Once

func updateOpenflow(conn db.Conn, myIP string) {
	var dbcs []db.Container
	var conns []db.Connection

	txn := func(view db.Database) error {
		conns = view.SelectFromConnection(nil)
		dbcs = view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.EndpointID != "" && dbc.IP != "" && dbc.Minion == myIP
		})
		return nil
	}
	conn.Txn(db.ConnectionTable, db.ContainerTable).Run(txn)

	ofcs := openflowContainers(dbcs, conns)
	if err := replaceFlows(ofcs); err != nil {
		log.WithError(err).Warning("Failed to update OpenFlow")
	}
}

func openflowContainers(dbcs []db.Container,
	conns []db.Connection) []openflow.Container {

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

	var ofcs []openflow.Container
	for _, dbc := range dbcs {
		_, peerKelda := ipdef.PatchPorts(dbc.EndpointID)

		ofc := openflow.Container{
			Veth:  ipdef.IFName(dbc.EndpointID),
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

// newVault gets a Vault client connected to the leader of the cluster.
var newVault = func(conn db.Conn) (vault.SecretStore, error) {
	etcds := conn.SelectFromEtcd(nil)
	if len(etcds) == 0 || etcds[0].LeaderIP == "" {
		return nil, errors.New("no cluster leader")
	}

	return vault.New(etcds[0].LeaderIP)
}

var replaceFlows = openflow.ReplaceFlows
