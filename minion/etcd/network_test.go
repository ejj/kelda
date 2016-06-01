package etcd

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"strconv"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/davecgh/go-spew/spew"
)

func TestUpdateEtcdContainers(t *testing.T) {
	store := newTestMock()
	store.Mkdir(minionDir)
	conn := db.New()
	var containers []db.Container
	conn.Transact(func(view db.Database) error {
		for i := 2; i < 5; i++ {
			c := view.InsertContainer()
			si := strconv.Itoa(i)
			c.DockerID = si
			c.IP = fmt.Sprintf("10.0.0.%s", si)
			c.Command = []string{"echo", si}
			c.Labels = []string{"red", "blue"}
			c.Env = map[string]string{"i": si}
			view.Commit(c)
		}
		containers = view.SelectFromContainer(nil)
		return nil
	})

	cs := []storeContainer{}
	for i := 2; i < 5; i++ {
		si := strconv.Itoa(i)
		storeTemp := storeContainer{
			DockerID: si,
			IP:       fmt.Sprintf("10.0.0.%s", si),
			Command:  []string{"echo", si},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"i": si},
		}
		cs = append(cs, storeTemp)
	}

	testContainers, _ := json.Marshal(cs)
	err := store.Set(containerStore, string(testContainers), 0)
	if err != nil {
		t.Fatal("Failed to write to store.")
	}

	*store.writes = 0
	etcdData, _ := readEtcd(store)
	etcdData, _ = updateEtcdContainer(store, etcdData, containers)

	resultContainers, err := store.Get(containerStore)
	if err != nil {
		t.Fatal("Failed to read from store")
	}

	resultSlice := []storeContainer{}
	json.Unmarshal([]byte(resultContainers), &resultSlice)

	if !eq(cs, resultSlice) {
		t.Error(spew.Sprintf("Unexpected change to etcd store. Got %v,"+
			" expected %v.", resultSlice, cs))
	}

	// etcd and the db agree, there should be no writes
	if *store.writes != 0 {
		t.Error(spew.Sprintf("Unexpected writes (%d) to etcd store.",
			*store.writes))
	}

	for i := range cs {
		if cs[i].DockerID == "3" {
			cs[i].IP = "10.0.0.7"
		}
	}
	// simulate etcd having out of date information, except the IP
	badEtcdSlice := []storeContainer{}
	for i := 2; i < 6; i++ {
		si := strconv.Itoa(i)
		badEtcd := storeContainer{
			DockerID: si,
			Command:  []string{"echo", si},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"i": si},
		}
		if i == 3 {
			si = "7"
		}
		badEtcd.IP = fmt.Sprintf("10.0.0.%s", si)
		badEtcdSlice = append(badEtcdSlice, badEtcd)
	}

	// add a new container with a bad ip to test ip syncing
	badEtcdSlice = append(badEtcdSlice, storeContainer{
		DockerID: "8",
		IP:       "10.0.0.0",
	})
	cs = append(cs, storeContainer{DockerID: "8", IP: "10.0.0.0"})
	conn.Transact(func(view db.Database) error {
		c := view.InsertContainer()
		c.DockerID = "8"
		c.IP = "junk"
		view.Commit(c)
		containers = append([]db.Container{c}, containers...)
		return nil
	})

	badTestContainers, _ := json.Marshal(badEtcdSlice)
	err = store.Set(containerStore, string(badTestContainers), 0)
	if err != nil {
		t.Fatalf("Failed to write to store.")
	}

	*store.writes = 0
	nextRand := uint32(0)
	rand32 = func() uint32 {
		ret := nextRand
		nextRand++
		return ret
	}

	defer func() {
		rand32 = rand.Uint32
	}()
	etcdData, _ = readEtcd(store)
	etcdData, _ = updateEtcdContainer(store, etcdData, containers)

	resultContainers, err = store.Get(containerStore)
	if err != nil {
		t.Fatal("Failed to read from store.")
	}

	resultSlice = []storeContainer{}
	json.Unmarshal([]byte(resultContainers), &resultSlice)

	if !eq(cs, resultSlice) {
		t.Error(spew.Sprintf("Expected change to etcd store.\nGot      %v"+
			"\nExpected %v.", resultSlice, cs))
	}

	if !eq(cs, etcdData.containers) {
		t.Error("updateEtcdContainer did change the storeData struct")
	}

	// if etcd and the db don't agree, there should be exactly 1 write
	if *store.writes != 1 {
		t.Error(spew.Sprintf("Expected a single write to etcd. Found %d.",
			*store.writes))
	}
}

func TestUpdateEtcdLabel(t *testing.T) {
	store := newTestMock()
	store.Mkdir(minionDir)
	conn := db.New()
	var containers []db.Container
	conn.Transact(func(view db.Database) error {
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
	testLabel, _ := json.Marshal(labelStruct)
	err := store.Set(labelToIPStore, string(testLabel), 0)
	if err != nil {
		t.Fatal("Failed to write to store.")
	}

	*store.writes = 0
	etcdData, _ := readEtcd(store)
	etcdData, _ = updateEtcdLabel(store, etcdData, containers)

	resultLabels, err := store.Get(labelToIPStore)
	if err != nil {
		t.Fatal("Failed to read from store")
	}

	resultStruct := map[string]string{}
	json.Unmarshal([]byte(resultLabels), &resultStruct)

	if !eq(labelStruct, resultStruct) {
		t.Error(spew.Sprintf("Unexpected change to etcd store. Got %v,"+
			" expected %v.", resultStruct, labelStruct))
	}

	// etcd and the db agree, there should be no writes
	if *store.writes != 0 {
		t.Error(spew.Sprintf("Unexpected writes (%d) to etcd store.",
			*store.writes))
	}

	conn.Transact(func(view db.Database) error {
		c := view.InsertContainer()
		c.DockerID = "6"
		c.Labels = []string{"2", "3"}
		view.Commit(c)
		containers = append(containers, c)
		return nil
	})

	// Label 2 is now multhost, so if etcd knows that, it should get etcd's ip
	labelStruct["2"] = "10.1.0.11"
	testLabel, _ = json.Marshal(labelStruct)
	err = store.Set(labelToIPStore, string(testLabel), 0)
	if err != nil {
		t.Fatal("Failed to write to store.")
	}

	// the dockerIP map still has label 3's IP, but label 3 is now multihost, so it
	// should get a new IP
	labelStruct["3"] = "10.1.0.0"

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
	if err != nil {
		t.Fatal("Failed to read from store.")
	}

	resultStruct = map[string]string{}
	json.Unmarshal([]byte(resultLabels), &resultStruct)

	if !eq(labelStruct, resultStruct) {
		t.Error(spew.Sprintf("Expected change to etcd store. Got %v, expected "+
			"%v.", resultStruct, labelStruct))
	}

	if !eq(labelStruct, etcdData.multiHost) {
		t.Error("updateEtcdLabel did not change storeData struct")
	}

	if *store.writes != 1 {
		t.Error(spew.Sprintf("Unexpected writes (%d) to etcd store.",
			*store.writes))
	}
}

func TestUpdateDBContainers(t *testing.T) {
	conn := db.New()
	conn.Transact(func(view db.Database) error {
		testUpdateDBContainers(t, view)
		return nil
	})
}

func testUpdateDBContainers(t *testing.T, view db.Database) {
	minion := view.InsertMinion()
	minion.Role = db.Worker
	minion.Self = true
	view.Commit(minion)

	for _, id := range []string{"a", "b"} {
		container := view.InsertContainer()
		container.DockerID = id
		view.Commit(container)
	}

	container := view.InsertContainer()
	container.DockerID = "c"
	container.IP = "junk"
	view.Commit(container)

	cs := storeContainerSlice{
		{
			DockerID: "a",
			IP:       "1.1.1.1",
			Command:  []string{"echo", "hi"},
			Labels:   []string{"red", "blue"},
			Env:      map[string]string{"GOPATH": "~/gocode"},
		}, {
			DockerID: "b",
			IP:       "2.2.2.2",
			Command:  []string{"echo", "bye"},
			Labels:   []string{"blue", "green"},
			Env:      map[string]string{"GOPATH": "~"},
		}}

	updateDBContainers(view, storeData{containers: cs})

	ipMap := map[string]string{}
	labelMap := map[string][]string{}
	commandMap := map[string][]string{}
	envMap := map[string]map[string]string{}
	for _, c := range view.SelectFromContainer(nil) {
		ipMap[c.DockerID] = c.IP
		labelMap[c.DockerID] = c.Labels
		commandMap[c.DockerID] = c.Command
		envMap[c.DockerID] = c.Env
	}

	expIPMap := map[string]string{
		"a": "1.1.1.1",
		"b": "2.2.2.2",
		"c": "",
	}
	if !eq(ipMap, expIPMap) {
		t.Error(spew.Sprintf("Found %s, Expected: %s", ipMap, expIPMap))
	}

	expLabelMap := map[string][]string{
		"a": {"red", "blue"},
		"b": {"blue", "green"},
		"c": nil,
	}

	if !eq(labelMap, expLabelMap) {
		t.Error(spew.Sprintf("Found %s, Expected: %s", labelMap, expLabelMap))
	}
}

func TestUpdateDBLabels(t *testing.T) {
	conn := db.New()
	conn.Transact(func(view db.Database) error {
		testUpdateDBLabels(t, view)
		return nil
	})
}

func testUpdateDBLabels(t *testing.T, view db.Database) {
	labelStruct := map[string]string{"a": "10.0.0.2"}
	containerSlice := []storeContainer{
		{
			DockerID: "hi",
			Labels:   []string{"a", "b"},
			IP:       "10.0.0.3",
		},
		{
			DockerID: "bye",
			Labels:   []string{"a"},
			IP:       "10.0.0.4",
		},
	}

	updateDBLabels(view, storeData{
		containers: containerSlice,
		multiHost:  labelStruct,
	})
	lip := map[string]string{}

	for _, l := range view.SelectFromLabel(nil) {
		if _, ok := lip[l.Label]; ok {
			t.Errorf("Duplicate labels in the DB: %s", l.Label)
		}
		lip[l.Label] = l.IP
	}

	resultLabels := map[string]string{"a": "10.0.0.2", "b": "10.0.0.3"}

	if !eq(lip, resultLabels) {
		t.Error(spew.Sprintf("Found: %v\nExpected: %v\n", lip, labelStruct))
	}
}

func TestSyncIPs(t *testing.T) {
	prefix := net.IPv4(10, 0, 0, 0)

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

	syncIPs(ipMap, prefix)

	// 10.0.0.1 is reserved for the default gateway
	exp := sliceToSet([]string{"10.0.0.0", "10.0.0.2", "10.0.0.3"})
	ipSet := map[string]struct{}{}
	for _, ip := range ipMap {
		ipSet[ip] = struct{}{}
	}

	if !eq(ipSet, exp) {
		t.Error(spew.Sprintf("Unexpected IP allocations."+
			"\nFound %s\nExpected %s\nMap %s",
			ipSet, exp, ipMap))
	}

	ipMap["a"] = "junk"

	syncIPs(ipMap, prefix)

	aIP := ipMap["a"]
	expected := "10.0.0.4"
	if aIP != expected {
		t.Error(spew.Sprintf("Unexpected IP allocations.\nFound %s\nExpected %s",
			aIP, expected))
	}

	// Force collisions
	rand32 = func() uint32 {
		return 4
	}

	ipMap["b"] = "junk"

	syncIPs(ipMap, prefix)

	if ip, _ := ipMap["b"]; ip != "" {
		t.Error(spew.Sprintf("Expected IP deletion, found %s", ip))
	}
}

func TestParseIP(t *testing.T) {
	res := parseIP("1.0.0.0", 0x01000000, 0xff000000)
	if res != 0x01000000 {
		t.Errorf("parseIP expected 0x%x, got 0x%x", 0x01000000, res)
	}

	res = parseIP("2.0.0.1", 0x01000000, 0xff000000)
	if res != 0 {
		t.Errorf("parseIP expected 0x%x, got 0x%x", 0, res)
	}

	res = parseIP("a", 0x01000000, 0xff000000)
	if res != 0 {
		t.Errorf("parseIP expected 0x%x, got 0x%x", 0, res)
	}
}

func TestRandomIP(t *testing.T) {
	prefix := uint32(0xaabbccdd)
	mask := uint32(0xfffff000)

	conflicts := map[uint32]struct{}{}

	// Only 4k IPs, in 0xfff00000. Guaranteed a collision
	for i := 0; i < 5000; i++ {
		ip := randomIP(conflicts, prefix, mask)
		if ip == 0 {
			continue
		}

		if _, ok := conflicts[ip]; ok {
			t.Fatalf("IP Double allocation: 0x%x", ip)
		}

		if prefix&mask != ip&mask {
			t.Fatalf("Bad IP allocation: 0x%x & 0x%x != 0x%x",
				ip, mask, prefix&mask)
		}

		conflicts[ip] = struct{}{}
	}

	if len(conflicts) < 2500 || len(conflicts) > 4096 {
		// If the code's working, this is possible but *extremely* unlikely.
		// Probably a bug.
		t.Errorf("Too few conflicts: %d", len(conflicts))
	}
}

func eq(a, b interface{}) bool {
	return reflect.DeepEqual(a, b)
}

func sliceToSet(slice []string) map[string]struct{} {
	res := map[string]struct{}{}
	for _, s := range slice {
		res[s] = struct{}{}
	}
	return res
}
