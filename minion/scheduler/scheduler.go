// Package scheduler is respnosible for deciding on which minion to place each container
// in the cluster.  It does this by updating each container in the Database with the
// PrivateIP of the minion it's assigned to, or the empty string if no assignment could
// be made.  Worker nodes then read these assignments form Etcd, and boot the containers
// that they are instructed to.
package scheduler

import (
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/util"
)

var c = counter.New("Scheduler")

// Run blocks implementing the scheduler module.
func Run(conn db.Conn, dk docker.Client) {
	loopLog := util.NewEventTimer("Scheduler")
	trig := conn.TriggerTick(60, db.MinionTable, db.ContainerTable,
		db.PlacementTable, db.EtcdTable, db.ImageTable).C
	for range trig {
		loopLog.LogStart()
		minion := conn.MinionSelf()

		if minion.Role == db.Master {
			runMaster(conn)
		}
		loopLog.LogEnd()
	}
}
