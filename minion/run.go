// +build !windows

package minion

import (
	"fmt"
	"time"

	"github.com/kelda/kelda/api"
	apiServer "github.com/kelda/kelda/api/server"
	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/connection"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/etcd"
	"github.com/kelda/kelda/minion/kubernetes"
	"github.com/kelda/kelda/minion/network"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/pprofile"
	"github.com/kelda/kelda/minion/registry"
	"github.com/kelda/kelda/minion/supervisor"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
)

var c = counter.New("Minion")

// Run blocks executing the minion.
func Run(role db.Role, inboundPubIntf, outboundPubIntf string) {
	// XXX Uncomment the following line to run the profiler
	//runProfiler(5 * time.Minute)

	conn := db.New()
	dk := docker.New("unix:///var/run/docker.sock")

	// XXX: As we are developing minion modules to use this passed down role
	// instead of querying their db independently, we need to do this.
	// Possibly in the future just pass down role into all of the modules,
	// but may be simpler to just have it use this entry.
	conn.Txn(db.MinionTable).Run(func(view db.Database) error {
		minion := view.InsertMinion()
		minion.Role = role
		minion.Self = true
		view.Commit(minion)
		return nil
	})

	if role == db.Worker {
		// Start writing the machine's subnets as soon as possible so that the
		// master can make informed IP allocations.
		go network.WriteSubnets(conn)
	}

	supervisor.Run(conn, dk, role)

	go network.Run(conn, inboundPubIntf, outboundPubIntf)
	go registry.Run(conn, dk)
	go etcd.Run(conn)
	go syncAuthorizedKeys(conn)
	if role == db.Worker {
		go openflow.Run(conn)
	}

	// Block until the credentials are in place on the local filesystem. We
	// can't simply fail if the first read fails because the daemon might still
	// be generating and copying keys onto the local filesystem. The key
	// installation is handled by SyncCredentials in cloud/credentials.go.
	var creds connection.Credentials
	err := util.BackoffWaitFor(func() bool {
		var err error
		creds, err = tlsIO.ReadCredentials(cliPath.MinionTLSDir)
		if err != nil {
			log.WithError(err).Debug("TLS keys not ready yet")
			return false
		}
		return true
	}, 30*time.Second, 1*time.Hour)
	if err != nil {
		log.Error("Failed to read minion credentials")
		return
	}

	go minionServerRun(conn, creds)
	go apiServer.Run(conn, fmt.Sprintf("tcp://0.0.0.0:%d", api.DefaultRemotePort),
		false, creds)

	if role == db.Master {
		go kubernetes.Run(conn, dk)
	}
	syncPolicy(conn)

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
