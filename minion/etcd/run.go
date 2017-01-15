package etcd

import (
	"encoding/json"

	"github.com/NetSys/quilt/db"
)

// Run synchronizes state in `conn` with the Etcd cluster.
func Run(conn db.Conn) {
	store := NewStore()
	makeEtcdDir(minionDir, store, 0)
	makeEtcdDir(subnetStore, store, 0)
	makeEtcdDir(nodeStore, store, 0)

	go runElection(conn, store)
	go runNetwork(conn, store)
	runMinionSync(conn, store)
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "    ")
}
