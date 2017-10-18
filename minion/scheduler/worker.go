package scheduler

import (
	"crypto/sha1"
	"fmt"
	"sync"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/network/plugin"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
)

const labelKey = "kelda"
const labelValue = "scheduler"
const labelPair = labelKey + "=" + labelValue
const filesKey = "files"
const concurrencyLimit = 32

var once sync.Once

func runWorker(conn db.Conn, dk docker.Client, myIP string) {
	if myIP == "" {
		return
	}

	// In order for the flows installed by the plugin to work, the basic flows must
	// already be installed.  Thus, the first time we run we pre-populate the
	// OpenFlow table.
	once.Do(func() {
		updateOpenflow(conn, myIP)
	})

	filter := map[string][]string{"label": {labelPair}}

	var toBoot, toKill []interface{}
	for i := 0; i < 2; i++ {
		dkcs, err := dk.List(filter)
		if err != nil {
			log.WithError(err).Warning("Failed to list docker containers.")
			return
		}

		conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
			dbcs := view.SelectFromContainer(func(dbc db.Container) bool {
				return dbc.IP != "" && dbc.Minion == myIP
			})

			var changed []db.Container
			changed, toBoot, toKill = syncWorker(dbcs, dkcs)
			for _, dbc := range changed {
				view.Commit(dbc)
			}

			return nil
		})

		if len(toBoot) == 0 && len(toKill) == 0 {
			break
		}

		start := time.Now()
		doContainers(dk, toKill, dockerKill)
		doContainers(dk, toBoot, dockerRun)
		log.Infof("Scheduler spent %v starting/stopping containers",
			time.Since(start))
	}

	updateOpenflow(conn, myIP)
}

func syncWorker(dbcs []db.Container, dkcs []docker.Container) (
	changed []db.Container, toBoot, toKill []interface{}) {

	var pairs []join.Pair
	pairs, toBoot, toKill = join.Join(dbcs, dkcs, syncJoinScore)

	for _, pair := range pairs {
		dbc := pair.L.(db.Container)
		dkc := pair.R.(docker.Container)

		dbc.DockerID = dkc.ID
		dbc.EndpointID = dkc.EID
		dbc.Status = dkc.Status
		dbc.Created = dkc.Created
		changed = append(changed, dbc)
	}

	return changed, toBoot, toKill
}

func doContainers(dk docker.Client, ifaces []interface{},
	do func(docker.Client, interface{})) {

	var wg sync.WaitGroup
	wg.Add(len(ifaces))
	defer wg.Wait()

	semaphore := make(chan struct{}, concurrencyLimit)
	for _, iface := range ifaces {
		semaphore <- struct{}{}
		go func(iface interface{}) {
			do(dk, iface)
			<-semaphore
			wg.Done()
		}(iface)
	}
}

func dockerRun(dk docker.Client, iface interface{}) {
	dbc := iface.(db.Container)
	log.WithField("container", dbc).Info("Start container")
	_, err := dk.Run(docker.RunOptions{
		Image:             dbc.Image,
		Args:              dbc.Command,
		Env:               dbc.Env,
		FilepathToContent: dbc.FilepathToContent,
		Labels: map[string]string{
			labelKey: labelValue,
			filesKey: filesHash(dbc.FilepathToContent),
		},
		IP:          dbc.IP,
		NetworkMode: plugin.NetworkName,
		DNS:         []string{ipdef.GatewayIP.String()},
		DNSSearch:   []string{"q"},
	})
	if err != nil {
		log.WithFields(log.Fields{
			"error":     err,
			"container": dbc,
		}).WithError(err).Warning("Failed to run container")
	}
}

func dockerKill(dk docker.Client, iface interface{}) {
	dkc := iface.(docker.Container)
	log.WithField("container", dkc.ID).Info("Remove container")
	if err := dk.RemoveID(dkc.ID); err != nil {
		log.WithFields(log.Fields{
			"error": err,
			"id":    dkc.ID,
		}).Warning("Failed to remove container.")
	}
}

func syncJoinScore(left, right interface{}) int {
	dbc := left.(db.Container)
	dkc := right.(docker.Container)

	if dbc.IP != dkc.IP || filesHash(dbc.FilepathToContent) != dkc.Labels[filesKey] {
		return -1
	}

	compareIDs := dbc.ImageID != ""
	namesMatch := dkc.Image == dbc.Image
	idsMatch := dkc.ImageID == dbc.ImageID
	if (compareIDs && !idsMatch) || (!compareIDs && !namesMatch) {
		return -1
	}

	for key, value := range dbc.Env {
		if dkc.Env[key] != value {
			return -1
		}
	}

	// Depending on the container, the command in the database could be
	// either the command plus it's arguments, or just it's arguments.  To
	// handle that case, we check both.
	cmd1 := dkc.Args
	cmd2 := append([]string{dkc.Path}, dkc.Args...)
	if len(dbc.Command) != 0 &&
		!util.StrSliceEqual(dbc.Command, cmd1) &&
		!util.StrSliceEqual(dbc.Command, cmd2) {
		return -1
	}

	return 0
}

func filesHash(filepathToContent map[string]string) string {
	toHash := util.MapAsString(filepathToContent)
	return fmt.Sprintf("%x", sha1.Sum([]byte(toHash)))
}

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
		if conn.From != blueprint.PublicInternetLabel &&
			conn.To != blueprint.PublicInternetLabel {
			continue
		}

		if conn.MinPort != conn.MaxPort {
			c.Inc("Unsupported Public Port Range")
			log.WithField("connection", conn).Debug(
				"Unsupported Public Port Range")
			continue
		}

		if conn.From == blueprint.PublicInternetLabel {
			fromPubPorts[conn.To] = append(fromPubPorts[conn.To],
				conn.MinPort)
		}

		if conn.To == blueprint.PublicInternetLabel {
			toPubPorts[conn.From] = append(toPubPorts[conn.From],
				conn.MinPort)
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

var replaceFlows = openflow.ReplaceFlows
