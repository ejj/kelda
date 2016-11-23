package db

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
)

func TestMachine(t *testing.T) {
	conn := New()

	var m Machine
	err := conn.Transact(func(db Database) error {
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

	err = conn.Transact(func(db Database) error {
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
	conn.Transact(func(db Database) error {
		for i := 0; i < 4; i++ {
			m := db.InsertMachine()
			m.Region = regions[i]
			db.Commit(m)
			machines = append(machines, m)
		}
		return nil
	})

	err := conn.Transact(func(db Database) error {
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

	err := conn.Transact(func(db Database) error {
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
	err = conn.Transact(func(db Database) error {
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
	err := conn.Transact(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.Fail()
		return
	}

	triggerRecv(t, mt)

	mt.Stop()
	err = conn.Transact(func(db Database) error {
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

func TestSortContainers(t *testing.T) {
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

	if !reflect.DeepEqual(SortContainers(containers), expected) {
		t.Errorf("Bad Container Sort: expected %q, got %q", expected, containers)
	}
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
