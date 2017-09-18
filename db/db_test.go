package db

import (
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/kelda/kelda/minion/pb"
	"github.com/stretchr/testify/assert"
)

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
		view.InsertLoadBalancer()
		view.InsertMinion()
		view.InsertMachine()
		view.InsertBlueprint()
		view.InsertPlacement()
		view.InsertContainer()
		view.InsertConnection()

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

	tr := New().Txn(MachineTable, BlueprintTable)
	tr.Run(func(view Database) error {
		view.InsertMachine()
		view.InsertBlueprint()

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

	tr := New().Txn(MachineTable, BlueprintTable)
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
	ct := conn.Trigger(BlueprintTable)
	ct2 := conn.Trigger(BlueprintTable)

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
		{BlueprintID: "3"},
		{BlueprintID: "5"},
		{BlueprintID: "5"},
		{BlueprintID: "1"},
	}
	expected := []Container{
		{BlueprintID: "1"},
		{BlueprintID: "3"},
		{BlueprintID: "5"},
		{BlueprintID: "5"},
	}

	sort.Sort(ContainerSlice(containers))
	assert.Equal(t, expected, containers)
	assert.Equal(t, containers[0], ContainerSlice(containers).Get(0))

	loadBalancers := []LoadBalancer{{Name: "b"}, {Name: "a"}}
	expLoadBalancers := []LoadBalancer{{Name: "a"}, {Name: "b"}}
	sort.Sort(LoadBalancerSlice(loadBalancers))
	assert.Equal(t, expLoadBalancers, loadBalancers)
	assert.Equal(t, loadBalancers[0], LoadBalancerSlice(loadBalancers).Get(0))

	conns := []Connection{{ID: 2}, {ID: 1}}
	expConns := []Connection{{ID: 1}, {ID: 2}}
	sort.Sort(ConnectionSlice(conns))
	assert.Equal(t, expConns, conns)
	assert.Equal(t, conns[0], ConnectionSlice(conns).Get(0))
}

func TestRole(t *testing.T) {
	t.Parallel()

	assert.Equal(t, pb.MinionConfig_NONE, RoleToPB(None))
	assert.Equal(t, pb.MinionConfig_WORKER, RoleToPB(Worker))
	assert.Equal(t, pb.MinionConfig_MASTER, RoleToPB(Master))

	assert.Equal(t, Role(None), PBToRole(pb.MinionConfig_NONE))
	assert.Equal(t, Role(Worker), PBToRole(pb.MinionConfig_WORKER))
	assert.Equal(t, Role(Master), PBToRole(pb.MinionConfig_MASTER))

	r, err := ParseRole("")
	assert.NoError(t, err)
	assert.Equal(t, Role(None), r)

	r, err = ParseRole("Worker")
	assert.NoError(t, err)
	assert.Equal(t, Role(Worker), r)

	r, err = ParseRole("Master")
	assert.NoError(t, err)
	assert.Equal(t, Role(Master), r)

	r, err = ParseRole("err")
	assert.Error(t, err)
	assert.Equal(t, Role(None), r)
}

func TestRowSlice(t *testing.T) {
	t.Parallel()

	rows := rowSlice([]row{Machine{ID: 1}, Machine{ID: 2}})
	assert.Equal(t, 2, rows.Len())
	assert.Equal(t, true, rows.Less(0, 1))
	rows.Swap(0, 1)
	assert.Equal(t, false, rows.Less(0, 1))
}
