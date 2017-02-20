// Package scheduler is respnosible for deciding on which minion to place each container
// in the cluster.  It does this by updating each container in the Database with the
// PrivateIP of the minion it's assigned to, or the empty string if no assignment could
// be made.  Worker nodes then read these assignments form Etcd, and boot the containers
// that they are instructed to.
package scheduler

import (
	"time"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/docker"
	"github.com/quilt/quilt/minion/network/plugin"
	"github.com/quilt/quilt/util"
	log "github.com/Sirupsen/logrus"
)

// Run blocks implementing the scheduler module.
func Run(conn db.Conn, dk docker.Client) {
	bootWait(conn)

	err := dk.ConfigureNetwork(plugin.NetworkName)
	if err != nil {
		log.WithError(err).Fatal("Failed to configure network plugin")
	}

	loopLog := util.NewEventTimer("Scheduler")
	trig := conn.TriggerTick(60, db.MinionTable, db.ContainerTable,
		db.PlacementTable, db.EtcdTable).C
	for range trig {
		loopLog.LogStart()
		minion, err := conn.MinionSelf()
		if err != nil {
			log.WithError(err).Warn("Missing self in the minion table.")
			continue
		}

		if minion.Role == db.Worker && minion.SupervisorInit {
			runWorker(conn, dk, minion.PrivateIP)
		} else if minion.Role == db.Master {
			runMaster(conn)
		}
		loopLog.LogEnd()
	}
}

func bootWait(conn db.Conn) {
	for workerCount := 0; workerCount <= 0; {
		workerCount = 0
		for _, m := range conn.SelectFromMinion(nil) {
			if m.Role == db.Worker {
				workerCount++
			}
		}
		time.Sleep(30 * time.Second)
	}
}
