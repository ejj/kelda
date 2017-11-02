package etcd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
)

const connectionPath = "/connections"

func runConnection(conn db.Conn, store Store) {
	etcdWatch := store.Watch(connectionPath, 1*time.Second)
	trigg := conn.TriggerTick(60, db.ConnectionTable)
	for range util.JoinNotifiers(trigg.C, etcdWatch) {
		if err := runConnectionOnce(conn, store); err != nil {
			log.WithError(err).Warn("Failed to sync connections with Etcd.")
		}
	}
}

func runConnectionOnce(conn db.Conn, store Store) error {
	etcdStr, err := readEtcdNode(store, connectionPath)
	if err != nil {
		return fmt.Errorf("etcd read error: %s", err)
	}

	if conn.EtcdLeader() {
		c.Inc("Run Connection Leader")
		slice := db.ConnectionSlice(conn.SelectFromConnection(nil))
		err = writeEtcdSlice(store, connectionPath, etcdStr, slice)
		if err != nil {
			return fmt.Errorf("etcd write error: %s", err)
		}
	} else {
		c.Inc("Run Connection Worker")
		var etcdConns []db.Connection
		json.Unmarshal([]byte(etcdStr), &etcdConns)
		conn.Txn(db.ConnectionTable).Run(func(view db.Database) error {
			joinConnections(view, etcdConns)
			return nil
		})
	}

	return nil
}

func joinConnections(view db.Database, etcdConns []db.Connection) {
	key := func(iface interface{}) interface{} {
		conn := iface.(db.Connection)
		return fmt.Sprintf("%s %s %d %d",
			conn.From, conn.To, conn.MinPort, conn.MaxPort)
	}

	_, connIfaces, etcdConnIfaces := join.HashJoin(
		db.ConnectionSlice(view.SelectFromConnection(nil)),
		db.ConnectionSlice(etcdConns), key, key)

	for _, conn := range connIfaces {
		view.Remove(conn.(db.Connection))
	}

	for _, etcdConnIface := range etcdConnIfaces {
		etcdConn := etcdConnIface.(db.Connection)
		conn := view.InsertConnection()
		etcdConn.ID = conn.ID
		view.Commit(etcdConn)
	}
}
