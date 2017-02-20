package minion

import (
	"fmt"
	"time"

	"github.com/quilt/quilt/api"
	apiServer "github.com/quilt/quilt/api/server"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/docker"
	"github.com/quilt/quilt/minion/etcd"
	"github.com/quilt/quilt/minion/network"
	"github.com/quilt/quilt/minion/network/plugin"
	"github.com/quilt/quilt/minion/pprofile"
	"github.com/quilt/quilt/minion/scheduler"
	"github.com/quilt/quilt/minion/supervisor"
	"github.com/quilt/quilt/util"

	log "github.com/Sirupsen/logrus"
)

// Run blocks executing the minion.
func Run() {
	// XXX Uncomment the following line to run the profiler
	//runProfiler(5 * time.Minute)

	log.Info("Minion Start")

	conn := db.New()
	dk := docker.New("unix:///var/run/docker.sock")

	// Not in a goroutine, want the plugin to start before the scheduler
	plugin.Run()

	go minionServerRun(conn)
	go supervisor.Run(conn, dk)
	go scheduler.Run(conn, dk)
	go network.Run(conn)
	go etcd.Run(conn)
	go syncAuthorizedKeys(conn)

	go apiServer.Run(conn, fmt.Sprintf("tcp://0.0.0.0:%d", api.DefaultRemotePort))

	loopLog := util.NewEventTimer("Minion-Update")

	for range conn.Trigger(db.MinionTable, db.EtcdTable).C {
		loopLog.LogStart()
		txn := conn.Txn(db.ConnectionTable, db.ContainerTable, db.MinionTable,
			db.EtcdTable, db.PlacementTable)
		txn.Run(func(view db.Database) error {
			minion, err := view.MinionSelf()
			if err == nil && view.EtcdLeader() {
				updatePolicy(view, minion.Spec)
			}
			return nil
		})
		loopLog.LogEnd()
	}
}

func runProfiler(duration time.Duration) {
	go func() {
		p := pprofile.New("minion")
		for {
			if err := p.TimedRun(duration); err != nil {
				log.Error(err)
			}
		}
	}()
}
