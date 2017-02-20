package scheduler

import (
	"sync"
	"time"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/minion/docker"
	"github.com/quilt/quilt/minion/ipdef"
	"github.com/quilt/quilt/minion/network/openflow"
	"github.com/quilt/quilt/minion/network/plugin"
	"github.com/quilt/quilt/util"
	log "github.com/Sirupsen/logrus"
)

const labelKey = "quilt"
const labelValue = "scheduler"
const labelPair = labelKey + "=" + labelValue
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
		doContainers(dk, toBoot, dockerRun)
		doContainers(dk, toKill, dockerKill)
		log.Infof("Scheduler spent %v starting/stopping containers",
			time.Since(start))
	}

	updateOpenflow(conn, myIP)
}

func syncWorker(dbcs []db.Container, dkcs []docker.Container) (
	changed []db.Container, toBoot, toKill []interface{}) {

	pairs, dbci, dkci := join.Join(dbcs, dkcs, syncJoinScore)

	for _, i := range dkci {
		toKill = append(toKill, i.(docker.Container))
	}

	for _, pair := range pairs {
		dbc := pair.L.(db.Container)
		dkc := pair.R.(docker.Container)

		dbc.DockerID = dkc.ID
		dbc.EndpointID = dkc.EID
		dbc.Status = dkc.Status
		dbc.Created = dkc.Created
		changed = append(changed, dbc)
	}

	for _, i := range dbci {
		dbc := i.(db.Container)
		toBoot = append(toBoot, dbc)
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
		Image:       dbc.Image,
		Args:        dbc.Command,
		Env:         dbc.Env,
		Labels:      map[string]string{labelKey: labelValue},
		IP:          dbc.IP,
		NetworkMode: plugin.NetworkName,
		DNS:         []string{ipdef.GatewayIP.String()},
		DNSSearch:   []string{"q"},
	})
	if err != nil {
		log.WithFields(log.Fields{
			"error":     err,
			"container": dbc,
		}).WithError(err).Warning("Failed to run container", dbc)
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

	if dbc.Image != dkc.Image || dbc.IP != dkc.IP {
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

func updateOpenflow(conn db.Conn, myIP string) {
	dbcs := conn.SelectFromContainer(func(dbc db.Container) bool {
		return dbc.EndpointID != "" && dbc.IP != "" && dbc.Minion == myIP
	})

	ofcs := openflowContainers(dbcs)
	if err := replaceFlows(ofcs); err != nil {
		log.WithError(err).Warning("Failed to update OpenFlow")
	}
}

func openflowContainers(dbcs []db.Container) []openflow.Container {
	var ofcs []openflow.Container
	for _, dbc := range dbcs {
		_, peerQuilt := ipdef.PatchPorts(dbc.EndpointID)
		ofcs = append(ofcs, openflow.Container{
			Veth:  ipdef.IFName(dbc.EndpointID),
			Patch: peerQuilt,
			Mac:   ipdef.IPStrToMac(dbc.IP)})
	}
	return ofcs
}

var replaceFlows = openflow.ReplaceFlows
