package etcd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/davecgh/go-spew/spew"
)

func TestWriteMinion(t *testing.T) {
	t.Parallel()

	ip := "1.2.3.4"
	key := "/minion/nodes/" + ip

	conn := db.New()
	store := NewMock()

	writeMinion(conn, store)
	val, err := store.Get(key)
	if err == nil {
		t.Errorf("Expected \"\", got %s", val)
	}

	// Minion without a PrivateIP
	conn.Transact(func(view db.Database) error {
		m := view.InsertMinion()
		m.Self = true
		m.Role = db.Master
		m.Provider = "Amazon"
		m.Size = "Big"
		m.Region = "Somewhere"
		view.Commit(m)
		return nil
	})

	writeMinion(conn, store)
	val, err = store.Get(key)
	if err == nil {
		t.Errorf("Expected \"\", got %s", val)
	}

	conn.Transact(func(view db.Database) error {
		m, _ := view.MinionSelf()
		m.PrivateIP = ip
		view.Commit(m)
		return nil
	})
	writeMinion(conn, store)
	val, err = store.Get(key)
	if err != nil {
		t.Errorf("Error %s", err)
	}

	expVal := `{"Role":"Master","PrivateIP":"1.2.3.4",` +
		`"Provider":"Amazon","Size":"Big","Region":"Somewhere"}`
	if val != expVal {
		t.Errorf("Got %s\n\tExp %s", val, expVal)
	}
}

func TestReadMinion(t *testing.T) {
	t.Parallel()

	conn := db.New()
	store := NewMock()

	m := randMinion()
	js, _ := json.Marshal(m)
	store.Set("/minion/nodes/foo", string(js), 0)

	readMinion(conn, store)
	minions := conn.SelectFromMinion(nil)
	if len(minions) != 1 {
		t.Error(spew.Sprintf("Wrong number of minions: %s", minions))
	}

	minions[0].ID = m.ID
	if !reflect.DeepEqual(minions[0], m) {
		t.Error(spew.Sprintf("Incorrect DB Minion: %s", minions))
	}

	store = NewMock()
	store.Mkdir("/minion/nodes")
	readMinion(conn, store)
	minions = conn.SelectFromMinion(nil)
	if len(minions) > 0 {
		t.Error(spew.Sprintf("Expected zero minions, found: %s", minions))
	}
}

func TestReadDiff(t *testing.T) {
	t.Parallel()

	add, del := diffMinion(nil, nil)
	if len(add) > 0 {
		t.Error(spew.Sprintf("Expected no additions, found: %s", add))
	}

	if len(del) > 0 {
		t.Error(spew.Sprintf("Expected no deletions, found: %s", del))
	}

	sharedEtcd := randMinion()
	sharedDbm := sharedEtcd
	sharedDbm.Spec = "Spec"
	sharedDbm.ID = 3

	etcd := []db.Minion{randMinion()}
	dbms := []db.Minion{dbMinion()}

	del, add = diffMinion(append(dbms, sharedDbm), append(etcd, sharedEtcd))

	if !reflect.DeepEqual(del, dbms) {
		t.Error(spew.Sprintf("Diff Deletion Found:\n\t%s\nExpected:\n\t%s",
			del, dbms))
	}

	if !reflect.DeepEqual(add, etcd) {
		t.Error(spew.Sprintf("Diff Addition Found:\n\t%s\nExpected:\n\t%s",
			add, etcd))
	}
}

func TestFilter(t *testing.T) {
	newDB, newEtcd := filterSelf(nil, nil)
	if len(newDB) > 0 || len(newEtcd) > 0 {
		t.Error(spew.Sprintf("Filter change unexpected: %s, %s", newDB, newEtcd))
	}

	dbms := []db.Minion{dbMinion(), dbMinion()}
	etcd := []db.Minion{randMinion(), randMinion()}

	newDB, newEtcd = filterSelf(dbms, etcd)
	if !reflect.DeepEqual(dbms, newDB) {
		t.Error(spew.Sprintf("No Self DB Found:\n\t%s\nExpected:\n\t%s",
			newDB, dbms))
	}
	if !reflect.DeepEqual(etcd, newEtcd) {
		t.Error(spew.Sprintf("No Self Etcd Found:\n\t%s\nExpected:\n\t%s",
			newEtcd, etcd))
	}

	self := randMinion()
	selfEtcd := self
	self.Self = true

	newDB, newEtcd = filterSelf(append(dbms, self), append(etcd, selfEtcd))
	if !reflect.DeepEqual(dbms, newDB) {
		t.Error(spew.Sprintf("No Self DB Found:\n\t%s\nExpected:\n\t%s",
			newDB, dbms))
	}
	if !reflect.DeepEqual(etcd, newEtcd) {
		t.Error(spew.Sprintf("No Self Etcd Found:\n\t%s\nExpected:\n\t%s",
			newEtcd, etcd))
	}
}

func randMinion() db.Minion {
	return db.Minion{
		Role:      "Worker",
		PrivateIP: randStr(),
		Provider:  randStr(),
		Size:      randStr(),
		Region:    randStr(),
	}
}

func dbMinion() db.Minion {
	dbm := randMinion()
	dbm.Spec = randStr()
	dbm.ID = rand.Int()
	return dbm
}

func randStr() string {
	return fmt.Sprintf("%d-%d-%d-%d", rand.Int(), rand.Int(), rand.Int(), rand.Int())
}
