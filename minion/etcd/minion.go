package etcd

import (
	"encoding/json"
	"path"
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
)

const (
	minionTimeout = 30
	minionPath    = "/minions"
)

func runMinionSync(conn db.Conn, store Store) {
	loopLog := util.NewEventTimer("Etcd")
	for range conn.TriggerTick(minionTimeout/2, db.MinionTable).C {
		loopLog.LogStart()
		writeMinion(conn, store)
		readMinion(conn, store)
		loopLog.LogEnd()
	}
}

func readMinion(conn db.Conn, store Store) {
	tree, err := store.GetTree(minionPath)
	if err != nil {
		log.WithError(err).Warning("Failed to get minions from Etcd.")
		return
	}

	var storeMinions []db.Minion
	for _, t := range tree.Children {
		var m db.Minion
		if err := json.Unmarshal([]byte(t.Value), &m); err != nil {
			log.WithField("json", t.Value).Warning("Failed to parse Minion.")
			continue
		}
		storeMinions = append(storeMinions, m)
	}

	conn.Txn(db.MinionTable).Run(func(view db.Database) error {
		dbms, sms := filterSelf(view.SelectFromMinion(nil), storeMinions)
		del, add := diffMinion(dbms, sms)

		for _, m := range del {
			view.Remove(m)
		}

		for _, m := range add {
			minion := view.InsertMinion()
			id := minion.ID
			minion = m
			minion.ID = id
			view.Commit(minion)
		}
		return nil
	})
}

func filterSelf(dbMinions, storeMinions []db.Minion) ([]db.Minion, []db.Minion) {
	var self db.Minion
	var sms, dbms []db.Minion

	for _, dbm := range dbMinions {
		if dbm.Self {
			self = dbm
		} else {
			dbms = append(dbms, dbm)
		}
	}

	for _, m := range storeMinions {
		if self.PrivateIP != m.PrivateIP {
			sms = append(sms, m)
		}
	}

	return dbms, sms
}

func diffMinion(dbMinions, storeMinions []db.Minion) (del, add []db.Minion) {
	key := func(iface interface{}) interface{} {
		m := iface.(db.Minion)
		m.ID = 0
		m.Spec = ""
		m.Self = false
		m.AuthorizedKeys = ""
		return m
	}

	_, lefts, rights := join.HashJoin(db.MinionSlice(dbMinions),
		db.MinionSlice(storeMinions), key, key)

	for _, left := range lefts {
		del = append(del, left.(db.Minion))
	}

	for _, right := range rights {
		add = append(add, right.(db.Minion))
	}

	return
}

func writeMinion(conn db.Conn, store Store) {
	minion, err := conn.MinionSelf()
	if err != nil || minion.PrivateIP == "" {
		return
	}

	js, err := jsonMarshal(minion)
	if err != nil {
		panic("Failed to convert Minion to JSON")
	}

	key := path.Join(minionPath, minion.PrivateIP)
	if err := store.Set(key, string(js), minionTimeout*time.Second); err != nil {
		log.Warningf("Failed to create: %s", key)
		return
	}
}
