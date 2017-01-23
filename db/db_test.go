package db

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
)

func TestMachine(t *testing.T) {
	conn := New()

	var m Machine
	err := conn.Txn(AllTables...).Run(func(db Database) error {
		m = db.InsertMachine()
		return nil
	})
	if err != nil {
		t.FailNow()
	}

	if m.ID != 1 || m.Role != None || m.CloudID != "" || m.PublicIP != "" ||
		m.PrivateIP != "" {
		t.Errorf("Invalid Machine: %s", spew.Sdump(m))
		return
	}

	old := m

	m.Role = Worker
	m.CloudID = "something"
	m.PublicIP = "1.2.3.4"
	m.PrivateIP = "5.6.7.8"

	err = conn.Txn(AllTables...).Run(func(db Database) error {
		if err := SelectMachineCheck(db, nil, []Machine{old}); err != nil {
			return err
		}

		db.Commit(m)

		if err := SelectMachineCheck(db, nil, []Machine{m}); err != nil {
			return err
		}

		db.Remove(m)

		if err := SelectMachineCheck(db, nil, []Machine{}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Error(err.Error())
		return
	}
}

func TestMachineSelect(t *testing.T) {
	conn := New()
	regions := []string{"here", "there", "anywhere", "everywhere"}

	var machines []Machine
	conn.Txn(AllTables...).Run(func(db Database) error {
		for i := 0; i < 4; i++ {
			m := db.InsertMachine()
			m.Region = regions[i]
			db.Commit(m)
			machines = append(machines, m)
		}
		return nil
	})

	err := conn.Txn(AllTables...).Run(func(db Database) error {
		err := SelectMachineCheck(db, func(m Machine) bool {
			return m.Region == "there"
		}, []Machine{machines[1]})
		if err != nil {
			return err
		}

		err = SelectMachineCheck(db, func(m Machine) bool {
			return m.Region != "there"
		}, []Machine{machines[0], machines[2], machines[3]})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Error(err.Error())
		return
	}
}

func TestMachineString(t *testing.T) {
	m := Machine{}

	got := m.String()
	exp := "Machine-0{  }"
	if got != exp {
		t.Errorf("\nGot: %s\nExp: %s", got, exp)
	}

	m = Machine{
		ID:        1,
		CloudID:   "CloudID1234",
		Provider:  "Amazon",
		Region:    "us-west-1",
		Size:      "m4.large",
		PublicIP:  "1.2.3.4",
		PrivateIP: "5.6.7.8",
		DiskSize:  56,
		Connected: true,
	}
	got = m.String()
	exp = "Machine-1{Amazon us-west-1 m4.large, CloudID1234, PublicIP=1.2.3.4," +
		" PrivateIP=5.6.7.8, Disk=56GB, Connected}"
	if got != exp {
		t.Errorf("\nGot: %s\nExp: %s", got, exp)
	}
}

func TestContainerString(t *testing.T) {
	c := Container{}
	got := c.String()
	exp := "Container-0{run }"
	assert.Equal(t, got, exp)

	fakeMap := make(map[string]string)
	fakeMap["test"] = "tester"
	fakeTime := time.Now()
	fakeTimeString := fakeTime.String()

	c = Container{
		ID:         1,
		IP:         "1.2.3.4",
		Minion:     "Test",
		EndpointID: "TestEndpoint",
		StitchID:   1,
		DockerID:   "DockerID",
		Image:      "test/test",
		Status:     "testing",
		Command:    []string{"run", "/bin/sh"},
		Labels:     []string{"label1"},
		Env:        fakeMap,
		Created:    fakeTime,
	}

	exp = "Container-1{run test/test run /bin/sh, DockerID: DockerID, " +
		"Minion: Test, StitchID: 1, IP: 1.2.3.4, Labels: [label1], " +
		"Env: map[test:tester], Status: testing, Created: " + fakeTimeString + "}"

	assert.Equal(t, exp, c.String())
}

func TestTxnBasic(t *testing.T) {
	conn := New()
	conn.Txn(AllTables...).Run(func(view Database) error {
		m := view.InsertMachine()
		m.Provider = "Amazon"
		view.Commit(m)

		return nil
	})

	conn.Txn(MachineTable).Run(func(view Database) error {
		machines := view.SelectFromMachine(func(m Machine) bool {
			return true
		})

		if len(machines) != 1 {
			t.Fatal("No machines in DB, should be 1")
		}
		if machines[0].Provider != "Amazon" {
			t.Fatal("Machine provider is not Amazon")
		}

		return nil
	})
}

func TestAllTablesNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatal("Transaction panicked on valid transaction")
		}
	}()

	conn := New()
	conn.Txn(AllTables...).Run(func(view Database) error {
		view.InsertEtcd()
		view.InsertLabel()
		view.InsertMinion()
		view.InsertMachine()
		view.InsertCluster()
		view.InsertPlacement()
		view.InsertContainer()
		view.InsertConnection()
		view.InsertACL()

		return nil
	})
}

// Transactions should not panic when accessing tables in their allowed set.
func TestTxnNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatal("Transaction panicked on valid tables")
		}
	}()

	tr := New().Txn(MachineTable, ClusterTable)
	tr.Run(func(view Database) error {
		view.InsertMachine()
		view.InsertCluster()

		return nil
	})
}

// Transactions should panic when accessing tables not in their allowed set.
func TestTxnPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Transaction didn't panic on invalid tables")
		}
	}()

	tr := New().Txn(MachineTable, ClusterTable)
	tr.Run(func(view Database) error {
		view.InsertEtcd()

		return nil
	})
}

// Transactions should be able to run concurrently if their table sets do not overlap.
// This test is not comprehensive; it is merely a basic check to see is anything
// is obviously wrong.
func TestTxnConcurrent(t *testing.T) {
	// Run the deadlock test multiple times to increase the odds of detecting a race
	// condition
	for i := 0; i < 10; i++ {
		checkIndependentTransacts(t)
	}
}

// Fails the test when the transactions deadlock.
func checkIndependentTransacts(t *testing.T) {
	transactOneStart := make(chan struct{})
	transactTwoDone := make(chan struct{})
	done := make(chan struct{})
	doneRoutines := make(chan struct{})
	defer close(doneRoutines)

	subTxnOne, subTxnTwo := getRandomTransactions(New())
	one := func() {
		subTxnOne.Run(func(view Database) error {
			close(transactOneStart)
			select {
			case <-transactTwoDone:
				break
			case <-doneRoutines:
				return nil // break out of this if it times out
			}
			return nil
		})

		close(done)
	}

	two := func() {
		// Wait for either the first transact to start or for timeout
		select {
		case <-transactOneStart:
			break
		case <-doneRoutines:
			return // break out of this if it times out
		}

		subTxnTwo.Run(func(view Database) error {
			return nil
		})

		close(transactTwoDone)
	}

	go one()
	go two()
	timeout := time.After(time.Second)
	select {
	case <-timeout:
		t.Fatal("Transactions deadlocked")
	case <-done:
		return
	}
}

// Test that Transactions with overlapping table sets run sequentially.
// This test is not comprehensive; it is merely a basic check to see is anything
// is obviously wrong.
func TestTxnSequential(t *testing.T) {
	// Run the sequential test multiple times to increase the odds of detecting a
	// race condition
	for i := 0; i < 10; i++ {
		checkTxnSequential(t)
	}
}

// Fails the test when the transactions run out of order.
func checkTxnSequential(t *testing.T) {
	subTxnOne, subTxnTwo := getRandomTransactions(New(),
		pickTwoTables(map[TableType]struct{}{})...)

	done := make(chan struct{})
	defer close(done)
	results := make(chan int)
	defer close(results)

	oneStarted := make(chan struct{})
	one := func() {
		subTxnOne.Run(func(view Database) error {
			close(oneStarted)
			time.Sleep(100 * time.Millisecond)
			select {
			case results <- 1:
				return nil
			case <-done:
				return nil
			}
		})
	}

	two := func() {
		subTxnTwo.Run(func(view Database) error {
			select {
			case results <- 2:
				return nil
			case <-done:
				return nil
			}
		})
	}

	check := make(chan bool)
	defer close(check)
	go func() {
		first := <-results
		second := <-results

		check <- (first == 1 && second == 2)
	}()

	go one()
	<-oneStarted
	go two()

	timeout := time.After(time.Second)
	select {
	case <-timeout:
		t.Fatal("Transactions timed out")
	case success := <-check:
		if !success {
			t.Fatal("Transactions ran concurrently")
		}
	}
}

func getRandomTransactions(conn Conn, tables ...TableType) (Transaction, Transaction) {
	taken := map[TableType]struct{}{}
	firstTables := pickTwoTables(taken)
	secondTables := pickTwoTables(taken)

	firstTables = append(firstTables, tables...)
	secondTables = append(secondTables, tables...)

	return conn.Txn(firstTables...), conn.Txn(secondTables...)
}

func pickTwoTables(taken map[TableType]struct{}) []TableType {
	tableCount := int32(len(AllTables))
	chosen := []TableType{}
	for len(chosen) < 2 {
		tt := AllTables[rand.Int31n(tableCount)]
		if _, ok := taken[tt]; ok {
			continue
		}

		taken[tt] = struct{}{}
		chosen = append(chosen, tt)
	}

	return chosen
}

func TestTrigger(t *testing.T) {
	conn := New()

	mt := conn.Trigger(MachineTable)
	mt2 := conn.Trigger(MachineTable)
	ct := conn.Trigger(ClusterTable)
	ct2 := conn.Trigger(ClusterTable)

	triggerNoRecv(t, mt)
	triggerNoRecv(t, mt2)
	triggerNoRecv(t, ct)
	triggerNoRecv(t, ct2)

	err := conn.Txn(AllTables...).Run(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.Fail()
		return
	}

	triggerRecv(t, mt)
	triggerRecv(t, mt2)
	triggerNoRecv(t, ct)
	triggerNoRecv(t, ct2)

	mt2.Stop()
	err = conn.Txn(AllTables...).Run(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.Fail()
		return
	}
	triggerRecv(t, mt)
	triggerNoRecv(t, mt2)

	mt.Stop()
	ct.Stop()
	ct2.Stop()

	fast := conn.TriggerTick(1, MachineTable)
	triggerRecv(t, fast)
	triggerRecv(t, fast)
	triggerRecv(t, fast)
}

func TestTriggerTickStop(t *testing.T) {
	conn := New()

	mt := conn.TriggerTick(100, MachineTable)

	// The initial tick.
	triggerRecv(t, mt)

	triggerNoRecv(t, mt)
	err := conn.Txn(AllTables...).Run(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.Fail()
		return
	}

	triggerRecv(t, mt)

	mt.Stop()
	err = conn.Txn(AllTables...).Run(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.Fail()
		return
	}
	triggerNoRecv(t, mt)
}

func triggerRecv(t *testing.T, trig Trigger) {
	select {
	case <-trig.C:
	case <-time.Tick(5 * time.Second):
		t.Error("Expected Receive")
	}
}

func triggerNoRecv(t *testing.T, trig Trigger) {
	select {
	case <-trig.C:
		t.Error("Unexpected Receive")
	case <-time.Tick(25 * time.Millisecond):
	}
}

func SelectMachineCheck(db Database, do func(Machine) bool, expected []Machine) error {
	query := db.SelectFromMachine(do)
	sort.Sort(mSort(expected))
	sort.Sort(mSort(query))
	if !reflect.DeepEqual(expected, query) {
		return fmt.Errorf("unexpected query result: %s\nExpected %s",
			spew.Sdump(query), spew.Sdump(expected))
	}

	return nil
}

type prefixedString struct {
	prefix string
	str    string
}

func (ps prefixedString) String() string {
	return ps.prefix + ps.str
}

type testStringerRow struct {
	ID         int
	FieldOne   string
	FieldTwo   int `rowStringer:"omit"`
	FieldThree int `rowStringer:"three: %s"`
	FieldFour  prefixedString
	FieldFive  int
}

func (r testStringerRow) String() string {
	return ""
}

func (r testStringerRow) getID() int {
	return -1
}

func (r testStringerRow) less(arg row) bool {
	return true
}

func TestStringer(t *testing.T) {
	testRow := testStringerRow{
		ID:         5,
		FieldOne:   "one",
		FieldThree: 3,

		// Should always omit.
		FieldTwo: 2,

		// Should evaluate String() method.
		FieldFour: prefixedString{"pre", "foo"},

		// Should omit because value is zero value.
		FieldFive: 0,
	}
	exp := "testStringerRow-5{FieldOne=one, three: 3, FieldFour=prefoo}"
	actual := defaultString(testRow)
	if exp != actual {
		t.Errorf("Bad defaultStringer output: expected %q, got %q.", exp, actual)
	}
}

func TestSliceHelpers(t *testing.T) {
	containers := []Container{
		{StitchID: 3},
		{StitchID: 5},
		{StitchID: 5},
		{StitchID: 1},
	}
	expected := []Container{
		{StitchID: 1},
		{StitchID: 3},
		{StitchID: 5},
		{StitchID: 5},
	}

	sort.Sort(ContainerSlice(containers))
	assert.Equal(t, expected, containers)
	assert.Equal(t, containers[0], ContainerSlice(containers).Get(0))

	labels := []Label{{Label: "b"}, {Label: "a"}}
	expLabels := []Label{{Label: "a"}, {Label: "b"}}
	sort.Sort(LabelSlice(labels))
	assert.Equal(t, expLabels, labels)
	assert.Equal(t, labels[0], LabelSlice(labels).Get(0))

	conns := []Connection{{ID: 2}, {ID: 1}}
	expConns := []Connection{{ID: 1}, {ID: 2}}
	sort.Sort(ConnectionSlice(conns))
	assert.Equal(t, expConns, conns)
	assert.Equal(t, conns[0], ConnectionSlice(conns).Get(0))
}

func TestGetClusterNamespace(t *testing.T) {
	conn := New()

	ns, err := conn.GetClusterNamespace()
	assert.NotNil(t, err)
	assert.Exactly(t, ns, "")

	conn.Txn(AllTables...).Run(func(view Database) error {
		clst := view.InsertCluster()
		clst.Namespace = "test"
		view.Commit(clst)
		return nil
	})

	ns, err = conn.GetClusterNamespace()
	assert.NoError(t, err)
	assert.Exactly(t, ns, "test")
}

type mSort []Machine

func (machines mSort) sort() {
	sort.Stable(machines)
}

func (machines mSort) Len() int {
	return len(machines)
}

func (machines mSort) Swap(i, j int) {
	machines[i], machines[j] = machines[j], machines[i]
}

func (machines mSort) Less(i, j int) bool {
	return machines[i].ID < machines[j].ID
}
