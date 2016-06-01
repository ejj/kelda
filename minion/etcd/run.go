package etcd

import (
	"time"

	"github.com/NetSys/quilt/db"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
)

// Run synchronizes state in `conn` with the Etcd cluster.
func Run(conn db.Conn) {
	store := NewStore()
	createMinionDir(store)

	go runElection(conn, store)
	go runNetwork(conn, store)
	runMinionSync(conn, store)
}

func createMinionDir(store Store) {
	for {
		err := store.Mkdir(minionDir)
		if err == nil {
			return
		}
		log.WithError(err).Debug()

		// If the directory already exists, no need to create it
		etcdErr, ok := err.(client.Error)
		if ok && etcdErr.Code == client.ErrorCodeNodeExist {
			return
		}

		time.Sleep(5 * time.Second)
	}
}
