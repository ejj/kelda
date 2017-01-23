package etcd

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"path"
	"testing"
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMinion(t *testing.T) {
	t.Parallel()

	ip := "1.2.3.4"
	key := "/nodes/" + ip + "/" + selfNode

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
	store.Set(nodeStore+"/foo/"+selfNode, string(js), 0)

	readMinion(conn, store)
	minions := conn.SelectFromMinion(nil)
	assert.Equal(t, 1, len(minions))

	minions[0].ID = m.ID
	assert.Equal(t, m, minions[0])

	store = NewMock()
	store.Mkdir(nodeStore, 0)
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

// Test that GenerateSubnet generates valid subnets, and passively test that it can
// generate enough unique subnets by generating 3500 out of the possible 4095
func TestGenerateSubnet(t *testing.T) {
	store := newTestMock()
	minionMaskBits, _ := ipdef.SubMask.Size()
	emptyBits := uint32(0xffffffff >> uint(minionMaskBits))
	for i := 0; i < 3500; i++ {
		subnet, err := generateSubnet(store, db.Minion{PrivateIP: "10.1.0.1"})
		require.Nil(t, err)

		require.True(t, ipdef.QuiltSubnet.Contains(subnet.IP))
		require.NotEqual(t, subnet.IP, ipdef.LabelSubnet.IP)
		require.Equal(t, subnet.Mask, ipdef.SubMask)

		subnetInt := binary.BigEndian.Uint32(subnet.IP.To4())
		require.Zero(t, subnetInt&emptyBits)
	}
}

func TestUpdateSubnet(t *testing.T) {
	store := newTestMock()
	conn := db.New()

	subnetTTL = time.Second
	defer func() {
		subnetTTL = 5 * time.Minute
	}()

	nextRand := uint32(0)
	subnetStart, _ := ipdef.SubMask.Size()
	rand32 = func() uint32 {
		ret := nextRand
		nextRand++
		return ret << (32 - uint(subnetStart)) // increment inside the subnet
	}

	defer func() {
		rand32 = rand.Uint32
	}()

	sleep = func(sleepTime time.Duration) {}
	defer func() {
		sleep = time.Sleep
	}()

	var m db.Minion
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m = view.InsertMinion()
		m.PrivateIP = "1.2.3.4"
		m.Role = db.Worker
		m.Self = true
		view.Commit(m)
		return nil
	})

	timeUpdateSubnet := func() chan struct{} {
		out := make(chan struct{})
		go func() {
			m = updateSubnet(conn, store, m)
			out <- struct{}{}
			close(out)
		}()
		return out
	}

	done := timeUpdateSubnet()
	timer := time.After(100 * time.Millisecond)
	select {
	case <-timer:
		t.Fatal("Timed out syncing subnet")
	case <-done:
		break
	}

	firstSubnet := net.IPv4(10, 0, 16, 0).String()
	secondSubnet := net.IPv4(10, 0, 32, 0).String()
	thirdSubnet := net.IPv4(10, 0, 48, 0).String()

	minion, err := store.Get(path.Join(subnetStore, firstSubnet))
	assert.Nil(t, err)
	assert.Equal(t, "1.2.3.4", minion)

	err = store.Set(path.Join(subnetStore, firstSubnet), "1.2.3.5", time.Minute)
	assert.Nil(t, err)

	done = timeUpdateSubnet()
	timer = time.After(100 * time.Millisecond)
	select {
	case <-timer:
		t.Fatal("Timed out syncing subnet")
	case <-done:
		break
	}

	minion, err = store.Get(path.Join(subnetStore, firstSubnet))
	assert.Nil(t, err)
	assert.Equal(t, "1.2.3.5", minion)

	minion, err = store.Get(path.Join(subnetStore, secondSubnet))
	assert.Nil(t, err)
	assert.Equal(t, "1.2.3.4", minion)

	store.advanceTime(2 * time.Second)

	done = timeUpdateSubnet()
	timer = time.After(100 * time.Millisecond)
	select {
	case <-timer:
		t.Fatal("Timed out syncing subnet")
	case <-done:
		break
	}

	_, err = store.Get(path.Join(subnetStore, secondSubnet))
	assert.NotNil(t, err)

	minion, err = store.Get(path.Join(subnetStore, thirdSubnet))
	assert.Nil(t, err)
	assert.Equal(t, "1.2.3.4", minion)
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
