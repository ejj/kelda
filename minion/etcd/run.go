package etcd

import (
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/coreos/etcd/client"

	log "github.com/Sirupsen/logrus"
)

// Run synchronizes state in `conn` with the Etcd cluster.
func Run(conn db.Conn) {
	store := NewStore()
	makeEtcdDir(subnetStore, store, 0)
	makeEtcdDir(nodeStore, store, 0)

	go runElection(conn, store)
	go runNetwork(conn, store)
	go runConnection(conn, store)
	runMinionSync(conn, store)
}

func makeEtcdDir(dir string, store Store, ttl time.Duration) {
	for {
		err := createEtcdDir(dir, store, ttl)
		if err == nil {
			break
		}

		log.WithError(err).Debug("Failed to create etcd dir")
		time.Sleep(5 * time.Second)
	}
}

func createEtcdDir(dir string, store Store, ttl time.Duration) error {
	err := store.Mkdir(dir, ttl)
	if err == nil {
		return nil
	}

	// If the directory already exists, no need to create it
	etcdErr, ok := err.(client.Error)
	if ok && etcdErr.Code == client.ErrorCodeNodeExist {
		return store.RefreshDir(dir, ttl)
	}

	return err
}
