package etcd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"

	log "github.com/Sirupsen/logrus"
)

const labelPath = "/labels"

func runLabel(conn db.Conn, store Store) {
	etcdWatch := store.Watch(labelPath, 1*time.Second)
	trigg := conn.TriggerTick(60, db.LabelTable)
	for range joinNotifiers(trigg.C, etcdWatch) {
		if err := runLabelOnce(conn, store); err != nil {
			log.WithError(err).Warn("Failed to sync labels with Etcd.")
		}
	}
}

func runLabelOnce(conn db.Conn, store Store) error {
	etcdStr, err := readEtcdNode(store, labelPath)
	if err != nil {
		return fmt.Errorf("etcd read error: %s", err)
	}

	if conn.EtcdLeader() {
		labels := conn.SelectFromLabel(func(l db.Label) bool {
			return l.IP != ""
		})

		err := writeEtcdSlice(store, labelPath, etcdStr, db.LabelSlice(labels))
		if err != nil {
			return fmt.Errorf("etcd write error: %s", err)
		}
	} else {
		var etcdLabels []db.Label
		json.Unmarshal([]byte(etcdStr), &etcdLabels)
		conn.Txn(db.LabelTable).Run(func(view db.Database) error {
			joinLabels(view, etcdLabels)
			return nil
		})
	}

	return nil
}

func joinLabels(view db.Database, etcdLabels []db.Label) {
	key := func(iface interface{}) interface{} {
		label := iface.(db.Label)
		return struct {
			Label        string
			IP           string
			ContainerIPs string
		}{
			Label:        label.Label,
			IP:           label.IP,
			ContainerIPs: fmt.Sprintf("%v", label.ContainerIPs),
		}
	}

	_, dbIfaces, etcdIfaces := join.HashJoin(
		db.LabelSlice(view.SelectFromLabel(nil)),
		db.LabelSlice(etcdLabels), key, key)

	for _, iface := range dbIfaces {
		view.Remove(iface.(db.Label))
	}

	for _, iface := range etcdIfaces {
		etcdLabel := iface.(db.Label)
		label := view.InsertLabel()
		etcdLabel.ID = label.ID
		view.Commit(etcdLabel)
	}
}
