package db

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
)

func TestMachine(t *testing.T) {
	conn := New()

	err := conn.Txn(AllTables...).Run(func(db Database) error {
		db.InsertMachine()
		return nil
	})
	if err != nil {
		t.FailNow()
	}

	dbms := MachineSlice(conn.SelectFromMachine(func(m Machine) bool {
		return true
	}))
	m := dbms.Get(0).(Machine)
	assert.Equal(t, 1, dbms.Len())

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
	m.Preemptible = true

	err = conn.Txn(AllTables...).Run(func(db Database) error {
		if err := SelectMachineCheck(db, nil, []Machine{old}); err != nil {
			return err
		}

		db.Commit(m)

		if err := SelectMachineCheck(db, nil, []Machine{m}); err != nil {
			return err
		}

		db.Remove(m)

		return SelectMachineCheck(db, nil, nil)
	})
	if err != nil {
		t.Error(err.Error())
		return
	}
}

func TestMachineSelect(t *testing.T) {
	conn := New()
	ids := []string{"here", "there", "anywhere", "everywhere"}

	var machines []Machine
	conn.Txn(AllTables...).Run(func(db Database) error {
		for i := 0; i < 4; i++ {
			m := db.InsertMachine()
			m.CloudID = ids[i]
			if i == 0 {
				m.Role = Master
			}
			db.Commit(m)
			machines = append(machines, m)
		}
		return nil
	})

	err := conn.Txn(AllTables...).Run(func(db Database) error {
		err := SelectMachineCheck(db, func(m Machine) bool {
			return m.CloudID == "there"
		}, []Machine{machines[1]})
		if err != nil {
			return err
		}

		err = SelectMachineCheck(db, func(m Machine) bool {
			return m.CloudID != "there"
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
		ID:          1,
		Role:        Worker,
		Preemptible: true,
		CloudID:     "CloudID1234",
		Provider:    "Amazon",
		Region:      "us-west-1",
		Size:        "m4.large",
		PublicIP:    "1.2.3.4",
		PrivateIP:   "5.6.7.8",
		FloatingIP:  "8.9.3.2",
		DiskSize:    56,
		Status:      Connected,
	}
	got = m.String()
	exp = "Machine-1{Worker, Amazon us-west-1 m4.large preemptible, " +
		"CloudID1234, PublicIP=1.2.3.4, PrivateIP=5.6.7.8, FloatingIP=8.9.3.2," +
		" Disk=56GB, connected}"
	if got != exp {
		t.Errorf("\nGot: %s\nExp: %s", got, exp)
	}
}

func SelectMachineCheck(db Database, do func(Machine) bool, expected []Machine) error {
	query := db.SelectFromMachine(do)
	expected = SortMachines(expected)
	query = SortMachines(query)
	if !reflect.DeepEqual(expected, query) {
		return fmt.Errorf("unexpected query result: %s\nExpected %s",
			spew.Sdump(query), spew.Sdump(expected))
	}

	return nil
}
