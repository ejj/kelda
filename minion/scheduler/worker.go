package scheduler

import (
	"net"
	"sync"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/minion/docker"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/network/plugin"
	"github.com/NetSys/quilt/util"
	log "github.com/Sirupsen/logrus"
)

const labelKey = "quilt"
const labelValue = "scheduler"
const labelPair = labelKey + "=" + labelValue
const concurrencyLimit = 32

func runWorker(conn db.Conn, dk docker.Client, myIP string, subnet net.IPNet) {
	if myIP == "" {
		return
	}

	filter := map[string][]string{"label": {labelPair}}

	var toBoot, toKill []interface{}
	for i := 0; i < 2; i++ {
		dkcs, err := dk.List(filter)
		if err != nil {
			log.WithError(err).Warning("Failed to list docker containers.")
			return
		}

		conn.Txn(db.ContainerTable,
			db.MinionTable).Run(func(view db.Database) error {

			_, err := view.MinionSelf()
			if err != nil {
				return nil
			}

			dbcs := view.SelectFromContainer(func(dbc db.Container) bool {
				return dbc.Minion == myIP
			})

			dkcs, badDcks := filterOnSubnet(subnet, dkcs)

			var changed []db.Container
			changed, toBoot, toKill = syncWorker(dbcs, dkcs, subnet)
			for _, dbc := range changed {
				view.Commit(dbc)
			}

			toKill = append(toKill, badDcks...)
			return nil
		})

		doContainers(dk, toBoot, dockerRun)
		doContainers(dk, toKill, dockerKill)
	}
}

func filterOnSubnet(subnet net.IPNet, dkcs []docker.Container) (good []docker.Container,
	bad []interface{}) {

	for _, dkc := range dkcs {
		dkIP := net.ParseIP(dkc.IP)
		if subnet.Contains(dkIP) {
			good = append(good, dkc)
		} else {
			bad = append(bad, dkc)
		}
	}

	return good, bad
}

func syncWorker(dbcs []db.Container, dkcs []docker.Container, subnet net.IPNet) (
	changed []db.Container, toBoot, toKill []interface{}) {

	pairs, dbci, dkci := join.Join(dbcs, dkcs, syncJoinScore)

	for _, i := range dkci {
		toKill = append(toKill, i.(docker.Container))
	}

	for _, pair := range pairs {
		dbc := pair.L.(db.Container)
		dkc := pair.R.(docker.Container)

		if dbc.DockerID != dkc.ID {
			dbc.DockerID = dkc.ID
			dbc.IP = dkc.IP
			dbc.EndpointID = dkc.EID
			changed = append(changed, dbc)
		}
	}

	for _, i := range dbci {
		dbc := i.(db.Container)
		toBoot = append(toBoot, dbc)
	}

	return changed, toBoot, toKill
}

func doContainers(dk docker.Client, containers []interface{},
	do func(docker.Client, chan interface{})) {

	in := make(chan interface{})
	var wg sync.WaitGroup
	wg.Add(concurrencyLimit)
	for i := 0; i < concurrencyLimit; i++ {
		go func() {
			do(dk, in)
			wg.Done()
		}()
	}

	for _, dbc := range containers {
		in <- dbc
	}
	close(in)
	wg.Wait()
}

func dockerRun(dk docker.Client, in chan interface{}) {
	for i := range in {
		dbc := i.(db.Container)
		log.WithField("container", dbc).Info("Start container")
		_, err := dk.Run(docker.RunOptions{
			Image:       dbc.Image,
			Args:        dbc.Command,
			Env:         dbc.Env,
			Labels:      map[string]string{labelKey: labelValue},
			NetworkMode: plugin.NetworkName,
			DNS:         []string{ipdef.GatewayIP.String()},
			DNSSearch:   []string{"q"},
		})
		if err != nil {
			log.WithFields(log.Fields{
				"error":     err,
				"container": dbc,
			}).WithError(err).Warning("Failed to run container", dbc)
			continue
		}
	}
}

func dockerKill(dk docker.Client, in chan interface{}) {
	for i := range in {
		dkc := i.(docker.Container)
		log.WithField("container", dkc.ID).Info("Remove container")
		if err := dk.RemoveID(dkc.ID); err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"id":    dkc.ID,
			}).Warning("Failed to remove container.")
		}
	}
}

func syncJoinScore(left, right interface{}) int {
	dbc := left.(db.Container)
	dkc := right.(docker.Container)

	// Depending on the container, the command in the database could be
	// either The command plus it's arguments, or just it's arguments.  To
	// handle that case, we check both.
	cmd1 := dkc.Args
	cmd2 := append([]string{dkc.Path}, dkc.Args...)
	dbcCmd := dbc.Command

	for key, value := range dbc.Env {
		if dkc.Env[key] != value {
			return -1
		}
	}

	switch {
	case dbc.Image != dkc.Image:
		return -1
	case len(dbcCmd) != 0 &&
		!util.StrSliceEqual(dbcCmd, cmd1) &&
		!util.StrSliceEqual(dbcCmd, cmd2):
		return -1
	case dbc.DockerID == dkc.ID:
		return 0
	default:
		return 1
	}
}
