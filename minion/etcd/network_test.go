package etcd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"path"
	"strconv"
	"testing"

	"github.com/NetSys/quilt/db"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateEtcdContainers(t *testing.T) {
	store := newTestMock()
	store.Mkdir(minionDir, 0)
	conn := db.New()
	var containers []db.Container
	idIPMap := map[string]string{}
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		for i := 2; i < 5; i++ {
			c := view.InsertContainer()
			si := strconv.Itoa(i)
			c.StitchID = i
			c.IP = fmt.Sprintf("10.0.0.%s", si)
			c.Command = []string{"echo", si}
			c.Labels = []string{"red", "blue"}
			c.Env = map[string]string{"i": si}
			view.Commit(c)
			idIPMap[si] = c.IP
		}
		containers = view.SelectFromContainer(nil)
		return nil
	})

	cs := []storeContainer{}
	for i := 2; i < 5; i++ {
		si := strconv.Itoa(i)
		storeTemp := storeContainer{
			StitchID: i,
			Command:  []string{"echo", si},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"i": si},
		}
		cs = append(cs, storeTemp)
	}

	testContainers, _ := jsonMarshal(cs)
	err := store.Set(containerStore, string(testContainers), 0)
	assert.Nil(t, err)

	*store.writes = 0
	etcdData, _ := readEtcd(store)
	etcdData, _ = updateEtcdContainer(store, etcdData, containers)

	resultContainers, err := store.Get(containerStore)
	assert.Nil(t, err)

	resultSlice := []storeContainer{}
	json.Unmarshal([]byte(resultContainers), &resultSlice)
	assert.Equal(t, cs, resultSlice)

	// etcd and the db agree, there should be no writes
	assert.Equal(t, 0, *store.writes)

	// simulate etcd having out of date information, except the IP
	badEtcdSlice := []storeContainer{}
	for i := 2; i < 6; i++ {
		si := strconv.Itoa(i)
		badEtcd := storeContainer{
			StitchID: i,
			Command:  []string{"echo", si},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"i": si},
		}
		badEtcdSlice = append(badEtcdSlice, badEtcd)
	}

	// add a new container with a bad ip to test ip syncing
	badEtcdSlice = append(badEtcdSlice, storeContainer{StitchID: 8})
	cs = append(cs, storeContainer{StitchID: 8})
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		c := view.InsertContainer()
		c.StitchID = 8
		c.IP = "10.0.0.0"
		view.Commit(c)
		containers = append([]db.Container{c}, containers...)
		return nil
	})
	idIPMap["8"] = "10.0.0.0"
	jsonIPMap, _ := jsonMarshal(idIPMap)
	minionDirKey := path.Join(nodeStore, "testMinion")
	store.Set(path.Join(minionDirKey, minionIPStore), string(jsonIPMap), 0)

	badTestContainers, _ := jsonMarshal(badEtcdSlice)
	err = store.Set(containerStore, string(badTestContainers), 0)
	assert.Nil(t, err)

	*store.writes = 0
	etcdData, _ = readEtcd(store)
	etcdData, _ = updateEtcdContainer(store, etcdData, containers)

	resultContainers, err = store.Get(containerStore)
	assert.Nil(t, err)

	resultSlice = []storeContainer{}
	json.Unmarshal([]byte(resultContainers), &resultSlice)

	assert.Equal(t, cs, resultSlice)
	assert.Equal(t, cs, etcdData.containers)

	// if etcd and the db don't agree, there should be exactly 1 write
	assert.Equal(t, 1, *store.writes)
}

func TestUpdateEtcdLabel(t *testing.T) {
	store := newTestMock()
	store.Mkdir(minionDir, 0)
	conn := db.New()
	var containers []db.Container
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		for i := 2; i < 5; i++ {
			c := view.InsertContainer()
			si := strconv.Itoa(i)
			c.DockerID = si
			c.Labels = []string{si}
			view.Commit(c)
		}
		containers = view.SelectFromContainer(nil)
		return nil
	})

	labelStruct := map[string]string{}
	testLabel, _ := jsonMarshal(labelStruct)
	err := store.Set(labelToIPStore, string(testLabel), 0)
	assert.Nil(t, err)

	*store.writes = 0
	etcdData, _ := readEtcd(store)
	etcdData, _ = updateEtcdLabel(store, etcdData, containers)

	resultLabels, err := store.Get(labelToIPStore)
	assert.Nil(t, err)

	resultStruct := map[string]string{}
	json.Unmarshal([]byte(resultLabels), &resultStruct)

	assert.Equal(t, labelStruct, resultStruct)

	// etcd and the db agree, there should be no writes
	assert.Equal(t, 0, *store.writes)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		c := view.InsertContainer()
		c.DockerID = "6"
		c.Labels = []string{"2", "3"}
		view.Commit(c)
		containers = append(containers, c)
		return nil
	})

	// Label 2 is now multhost, so if etcd knows that, it should get etcd's ip
	labelStruct["2"] = "10.0.0.11"
	testLabel, _ = jsonMarshal(labelStruct)
	err = store.Set(labelToIPStore, string(testLabel), 0)
	assert.Nil(t, err)

	// the dockerIP map still has label 3's IP, but label 3 is now multihost, so it
	// should get a new IP
	labelStruct["3"] = "10.0.0.0"

	*store.writes = 0
	etcdData, _ = readEtcd(store)
	nextRand := uint32(0)
	rand32 = func() uint32 {
		ret := nextRand
		nextRand++
		return ret
	}

	defer func() {
		rand32 = rand.Uint32
	}()

	etcdData, _ = updateEtcdLabel(store, etcdData, containers)

	resultLabels, err = store.Get(labelToIPStore)
	assert.Nil(t, err)

	resultStruct = map[string]string{}
	json.Unmarshal([]byte(resultLabels), &resultStruct)

	assert.Equal(t, labelStruct, resultStruct)
	assert.Equal(t, labelStruct, etcdData.multiHost)
	assert.Equal(t, 1, *store.writes)
}

func TestUpdateLeaderDBC(t *testing.T) {
	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		dbc := view.InsertContainer()
		dbc.StitchID = 1
		view.Commit(dbc)

		updateLeaderDBC(view, view.SelectFromContainer(nil), storeData{
			containers: []storeContainer{{StitchID: 1}},
		}, map[string]string{"1": "foo"})

		dbcs := view.SelectFromContainer(nil)
		if len(dbcs) != 1 || dbcs[0].StitchID != 1 || dbcs[0].IP != "foo" ||
			dbcs[0].Mac != "" {
			t.Error(spew.Sprintf("Unexpected dbc: %v", dbc))
		}

		return nil
	})
}

func TestUpdateWorkerDBC(t *testing.T) {
	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		testUpdateWorkerDBC(t, view)
		return nil
	})
}

func testUpdateWorkerDBC(t *testing.T, view db.Database) {
	minion := view.InsertMinion()
	minion.Role = db.Worker
	minion.Self = true
	view.Commit(minion)

	for id := 1; id < 3; id++ {
		container := view.InsertContainer()
		container.StitchID = id
		container.IP = fmt.Sprintf("10.1.0.%d", id-1)
		container.Minion = "1.2.3.4"
		container.Command = []string{"echo", "hi"}
		container.Env = map[string]string{"GOPATH": "~"}
		view.Commit(container)
	}

	cs := storeContainerSlice{
		{
			StitchID: 1,
			Command:  []string{"echo", "hi"},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"GOPATH": "~"},
			Minion:   "1.2.3.4",
		}, {
			StitchID: 2,
			Command:  []string{"echo", "hi"},
			Labels:   []string{"blue", "green"},
			Env:      map[string]string{"GOPATH": "~"},
			Minion:   "1.2.3.4",
		}, {
			StitchID: 3,
			Command:  []string{"echo", "bye"},
			Labels:   []string{"blue", "green"},
			Env:      map[string]string{"GOPATH": "~"},
			Minion:   "1.2.3.5",
		},
	}

	store := newTestMock()

	jsonCS, _ := jsonMarshal(cs)
	err := store.Set(containerStore, string(jsonCS), 0)
	assert.Nil(t, err)

	jsonNull, _ := jsonMarshal(map[string]string{})
	minionDirKey := path.Join(nodeStore, "1.2.3.4")
	err = store.Set(path.Join(minionDirKey, minionIPStore), string(jsonNull), 0)
	assert.Nil(t, err)

	updateWorker(view, db.Minion{PrivateIP: "1.2.3.4",
		Subnet: "10.1.0.0"}, store, storeData{containers: cs})

	ipMap := map[int]string{}
	labelMap := map[int][]string{}
	commandMap := map[string][]string{}
	envMap := map[string]map[string]string{}
	for _, c := range view.SelectFromContainer(nil) {
		ipMap[c.StitchID] = c.IP
		labelMap[c.StitchID] = c.Labels
		commandMap[c.DockerID] = c.Command
		envMap[c.DockerID] = c.Env
	}

	expIPMap := map[int]string{
		1: "10.1.0.0",
		2: "10.1.0.1",
	}

	assert.Equal(t, expIPMap, ipMap)

	resultMap := map[string]string{}
	storeIPs, _ := store.Get(path.Join(minionDirKey, minionIPStore))
	json.Unmarshal([]byte(storeIPs), &resultMap)

	for id, ip := range resultMap {
		sid, _ := strconv.Atoi(id)
		if otherIP, ok := ipMap[sid]; !ok || ip != otherIP {
			t.Fatalf("IPs did not match: %s vs %s", ip, otherIP)
		}
	}

	expLabelMap := map[int][]string{
		1: {"red", "blue"},
		2: {"blue", "green"},
	}

	assert.Equal(t, expLabelMap, labelMap)
}

func TestContainerJoinScore(t *testing.T) {
	t.Parallel()

	a := storeContainer{
		Minion:   "Minion",
		Image:    "Image",
		StitchID: 1,
	}
	b := a

	score := containerJoinScore(a, b)
	assert.Equal(t, 0, score)

	b.Image = "Wrong"
	score = containerJoinScore(a, b)
	assert.Equal(t, -1, score)
}

func TestUpdateDBLabels(t *testing.T) {
	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		testUpdateDBLabels(t, view)
		return nil
	})
}

func testUpdateDBLabels(t *testing.T, view db.Database) {
	labelStruct := map[string]string{"a": "10.0.0.2"}
	ipMap := map[string]string{"1": "10.0.0.3", "2": "10.0.0.4"}
	containerSlice := []storeContainer{
		{
			StitchID: 1,
			Labels:   []string{"a", "b"},
		},
		{
			StitchID: 2,
			Labels:   []string{"a"},
		},
	}

	updateDBLabels(view, storeData{
		containers: containerSlice,
		multiHost:  labelStruct,
	}, ipMap)

	type labelIPs struct {
		labelIP      string
		containerIPs []string
	}
	lip := map[string]labelIPs{}
	for _, l := range view.SelectFromLabel(nil) {
		_, ok := lip[l.Label]
		assert.False(t, ok)

		lip[l.Label] = labelIPs{
			labelIP:      l.IP,
			containerIPs: l.ContainerIPs,
		}
	}

	resultLabels := map[string]labelIPs{
		"a": {
			labelIP:      "10.0.0.2",
			containerIPs: []string{"10.0.0.3", "10.0.0.4"},
		},
		"b": {
			labelIP:      "10.0.0.3",
			containerIPs: []string{"10.0.0.3"},
		},
	}

	assert.Equal(t, resultLabels, lip)
}

func TestSyncIPs(t *testing.T) {
	nextRand := uint32(0)
	rand32 = func() uint32 {
		ret := nextRand
		nextRand++
		return ret
	}

	defer func() {
		rand32 = rand.Uint32
	}()

	ipMap := map[string]string{
		"a": "",
		"b": "",
		"c": "",
	}

	syncLabelIPs(ipMap)

	// 10.0.0.1 is reserved for the default gateway
	exp := sliceToSet([]string{"10.0.0.0", "10.0.0.2", "10.0.0.3"})
	ipSet := map[string]struct{}{}
	for _, ip := range ipMap {
		ipSet[ip] = struct{}{}
	}

	assert.Equal(t, exp, ipSet)

	ipMap["d"] = "junk"

	syncLabelIPs(ipMap)

	aIP := ipMap["d"]
	expected := "10.0.0.4"
	assert.Equal(t, expected, aIP)

	// Force collisions
	rand32 = func() uint32 {
		return 4
	}

	ipMap["a"] = "junk"
	syncLabelIPs(ipMap)
	assert.NotEqual(t, "", ipMap["a"])

	ipMap["e"] = "junk"
	syncLabelIPs(ipMap)
	assert.Empty(t, "", ipMap["e"])
}

func TestAllocate(t *testing.T) {
	subnet := net.IPNet{
		IP:   net.IPv4(0xab, 0xcd, 0xe0, 0x00),
		Mask: net.CIDRMask(20, 32),
	}
	conflicts := map[string]struct{}{}
	ipSet := map[string]struct{}{}

	// Only 4k IPs, in 0xfffff000. Guaranteed a collision
	for i := 0; i < 5000; i++ {
		ip, err := allocateIP(ipSet, subnet)
		if err != nil {
			continue
		}

		if _, ok := conflicts[ip]; ok {
			t.Fatalf("IP Double allocation: 0x%x", ip)
		}

		require.True(t, subnet.Contains(net.ParseIP(ip)),
			fmt.Sprintf("\"%s\" is not in %s", ip, subnet))
		conflicts[ip] = struct{}{}
	}

	assert.Equal(t, len(conflicts), len(ipSet))
	if len(conflicts) < 2500 || len(conflicts) > 4096 {
		// If the code's working, this is possible but *extremely* unlikely.
		// Probably a bug.
		t.Errorf("Too few conflicts: %d", len(conflicts))
	}
}

func sliceToSet(slice []string) map[string]struct{} {
	res := map[string]struct{}{}
	for _, s := range slice {
		res[s] = struct{}{}
	}
	return res
}
