package etcd

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/stretchr/testify/assert"
)

func TestWriteMinion(t *testing.T) {
	t.Parallel()

	ip := "1.2.3.4"
	key := "/minions/" + ip + "/" + selfNode

	conn := db.New()
	store := NewMock()

	writeMinion(conn, store)
	val, err := store.Get(key)
	assert.NotNil(t, err)
	assert.Empty(t, val)

	// Minion without a PrivateIP
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
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
	assert.NotNil(t, err)
	assert.Empty(t, val)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m, _ := view.MinionSelf()
		m.PrivateIP = ip
		view.Commit(m)
		return nil
	})
	writeMinion(conn, store)
	val, err = store.Get(key)
	assert.Nil(t, err)

	expVal := `{
    "Role": "Master",
    "PrivateIP": "1.2.3.4",
    "Provider": "Amazon",
    "Size": "Big",
    "Region": "Somewhere"
}`
	assert.Equal(t, expVal, val)
}

func TestReadMinion(t *testing.T) {
	t.Parallel()

	conn := db.New()
	store := NewMock()

	m := randMinion()
	js, _ := jsonMarshal(m)
	store.Set(minionPath+"/foo/"+selfNode, string(js), 0)

	readMinion(conn, store)
	minions := conn.SelectFromMinion(nil)
	assert.Equal(t, 1, len(minions))

	minions[0].ID = m.ID
	assert.Equal(t, m, minions[0])

	store = NewMock()
	store.Mkdir(minionPath, 0)
	readMinion(conn, store)
	minions = conn.SelectFromMinion(nil)
	assert.Empty(t, minions)
}

func TestReadDiff(t *testing.T) {
	t.Parallel()

	add, del := diffMinion(nil, nil)
	assert.Empty(t, add)
	assert.Empty(t, del)

	sharedEtcd := randMinion()
	sharedDbm := sharedEtcd
	sharedDbm.Spec = "Spec"
	sharedDbm.ID = 3

	etcd := []db.Minion{randMinion()}
	dbms := []db.Minion{dbMinion()}

	del, add = diffMinion(append(dbms, sharedDbm), append(etcd, sharedEtcd))
	assert.Equal(t, dbms, del)
	assert.Equal(t, etcd, add)
}

func TestFilter(t *testing.T) {
	newDB, newEtcd := filterSelf(nil, nil)
	assert.Empty(t, newDB)
	assert.Empty(t, newEtcd)

	dbms := []db.Minion{dbMinion(), dbMinion()}
	etcd := []db.Minion{randMinion(), randMinion()}

	newDB, newEtcd = filterSelf(dbms, etcd)
	assert.Equal(t, dbms, newDB)
	assert.Equal(t, etcd, newEtcd)

	self := randMinion()
	selfEtcd := self
	self.Self = true

	newDB, newEtcd = filterSelf(append(dbms, self), append(etcd, selfEtcd))
	assert.Equal(t, dbms, newDB)
	assert.Equal(t, etcd, newEtcd)
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
