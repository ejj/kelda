package etcd

import (
	"encoding/json"
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"

	log "github.com/Sirupsen/logrus"
)

const timeout = 30

func runMinionSync(conn db.Conn, store Store) {
	for range conn.TriggerTick(timeout/2, db.MinionTable).C {
		writeMinion(conn, store)
		readMinion(conn, store)
	}
}

func readMinion(conn db.Conn, store Store) {
	tree, err := store.GetTree("/minion/nodes")
	if err != nil {
		log.Warning("Failed to get minions from Etcd.")
		return
	}

	var storeMinions []db.Minion
	for _, t := range tree.Children {
		var m db.Minion
		if err := json.Unmarshal([]byte(t.Value), &m); err != nil {
			log.Warning("Failed to parse Etcd Minion JSON: %s", t.Value)
			continue
		}
		storeMinions = append(storeMinions, m)
	}

	conn.Transact(func(view db.Database) error {
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
	if err != nil {
		return
	}

	if minion.PrivateIP == "" {
		return
	}

	js, err := json.Marshal(minion)
	if err != nil {
		panic("Failed to convert Minion to JSON")
	}

	key := "/minion/nodes/" + minion.PrivateIP
	if err := store.Set(key, string(js), timeout*time.Second); err != nil {
		log.Warning("Failed to update minion node in Etcd: %s", err)
	}
}
