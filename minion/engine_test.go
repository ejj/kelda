package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"text/scanner"
	"time"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/stitch"
	"github.com/NetSys/quilt/util"
	"github.com/davecgh/go-spew/spew"
)

const testImage = "alpine"

func TestContainerTxn(t *testing.T) {
	conn := db.New()
	trigg := conn.Trigger(db.ContainerTable).C

	spec := ""
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "a" (docker "alpine" "tail"))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "b" (docker "alpine" "tail"))
		 (label "a" "b" (docker "alpine" "tail"))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	spec = `(label "b" (docker "alpine" "cat"))
		 (label "a" "b" (docker "ubuntu" "tail"))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	spec = `(label "b" (docker "ubuntu" "cat"))
		 (label "a" "b" (docker "alpine" "tail"))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	spec = `(label "a" (makeList 2 (docker "alpine" "cat")))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	spec = `(label "a" (docker "alpine"))`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	spec = `(label "b" (docker "alpine"))
	        (label "c" (docker "alpine"))
	        (label "a" "b" "c")`
	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}

	if err := testContainerTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}
}

func testContainerTxn(conn db.Conn, spec string) string {
	var containers []db.Container
	conn.Transact(func(view db.Database) error {
		updatePolicy(view, db.Master, spec)
		containers = view.SelectFromContainer(nil)
		return nil
	})

	var sc scanner.Scanner
	compiled, err := stitch.New(*sc.Init(strings.NewReader(spec)), "", false)
	if err != nil {
		return err.Error()
	}

	for _, e := range queryContainers(compiled) {
		found := false
		for i, c := range containers {
			if e.Image == c.Image &&
				reflect.DeepEqual(e.Command, c.Command) &&
				util.EditDistance(c.Labels, e.Labels) == 0 {
				containers = append(containers[:i], containers[i+1:]...)
				found = true
				break
			}
		}

		if found == false {
			return fmt.Sprintf("Missing expected label set: %v\n%v",
				e, containers)
		}
	}

	if len(containers) > 0 {
		return spew.Sprintf("Unexpected containers: %s", containers)
	}

	return ""
}

func TestConnectionTxn(t *testing.T) {
	conn := db.New()
	trigg := conn.Trigger(db.ConnectionTable).C

	spec := ""
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "a" (docker "alpine"))
	        (connect 80 "a" "a")`
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "a" (docker "alpine"))
	        (connect 90 "a" "a")`
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "a" (docker "alpine"))
                (label "b" (docker "alpine"))
                (label "c" (docker "alpine"))
	        (connect 90 "b" "a" "c")
	        (connect 100 "b" "b")
	        (connect 101 "c" "a")`
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}

	spec = `(label "a" (docker "alpine"))
                (label "b" (docker "alpine"))
                (label "c" (docker "alpine"))`
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if !fired(trigg) {
		t.Error("Expected Database Change")
	}
	if err := testConnectionTxn(conn, spec); err != "" {
		t.Error(err)
	}
	if fired(trigg) {
		t.Error("Unexpected Database Change")
	}
}

func testConnectionTxn(conn db.Conn, spec string) string {
	var connections []db.Connection
	conn.Transact(func(view db.Database) error {
		updatePolicy(view, db.Master, spec)
		connections = view.SelectFromConnection(nil)
		return nil
	})

	var sc scanner.Scanner
	compiled, err := stitch.New(*sc.Init(strings.NewReader(spec)), "", false)
	if err != nil {
		return err.Error()
	}

	exp := compiled.QueryConnections()
	for _, e := range exp {
		found := false
		for i, c := range connections {
			if e.From == c.From && e.To == c.To && e.MinPort == c.MinPort &&
				e.MaxPort == c.MaxPort {
				connections = append(
					connections[:i], connections[i+1:]...)
				found = true
				break
			}
		}

		if found == false {
			return fmt.Sprintf("Missing expected connection: %v", e)
		}
	}

	if len(connections) > 0 {
		return spew.Sprintf("Unexpected connections: %s", connections)
	}

	return ""
}

func fired(c chan struct{}) bool {
	time.Sleep(5 * time.Millisecond)
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func TestPlacementTxn(t *testing.T) {
	conn := db.New()
	checkPlacement := func(spec string, exp ...db.Placement) {
		placements := map[db.Placement]struct{}{}
		conn.Transact(func(view db.Database) error {
			updatePolicy(view, db.Master, spec)
			res := view.SelectFromPlacement(nil)

			// Set the ID to 0 so that we can use reflect.DeepEqual.
			for _, p := range res {
				p.ID = 0
				placements[p] = struct{}{}
			}

			return nil
		})

		if len(placements) != len(exp) {
			t.Errorf("Placement error in %s. Expected %v, got %v",
				spec, exp, placements)
		}

		for _, p := range exp {
			if _, ok := placements[p]; !ok {
				t.Errorf("Placement error in %s. Expected %v, got %v",
					spec, exp, placements)
				break
			}
		}
	}

	// Create an exclusive placement.
	spec := `(label "foo" (docker "foo"))
	(label "bar" (docker "bar"))
	(place (labelRule "exclusive" "foo") "bar")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "bar",
			Exclusive:   true,
			OtherLabel:  "foo",
		},
	)

	// Change the placement from "exclusive" to "on".
	spec = `(label "foo" (docker "foo"))
	(label "bar" (docker "bar"))
	(place (labelRule "on" "foo") "bar")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "bar",
			Exclusive:   false,
			OtherLabel:  "foo",
		},
	)

	// Add another placement constraint.
	spec = `(label "foo" (docker "foo"))
	(label "bar" (docker "bar"))
	(place (labelRule "on" "foo") "bar")
	(place (labelRule "exclusive" "bar") "bar")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "bar",
			Exclusive:   false,
			OtherLabel:  "foo",
		},
		db.Placement{
			TargetLabel: "bar",
			Exclusive:   true,
			OtherLabel:  "bar",
		},
	)

	// Multiple placement targets.
	spec = `(label "foo" (docker "foo"))
	(label "bar" (docker "bar"))
	(label "qux" (docker "qux"))
	(place (labelRule "exclusive" "qux") "foo" "bar")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "bar",
			Exclusive:   true,
			OtherLabel:  "qux",
		},
		db.Placement{
			TargetLabel: "foo",
			Exclusive:   true,
			OtherLabel:  "qux",
		},
	)

	// Multiple exclusive labels.
	spec = `(label "foo" (docker "foo"))
	(label "bar" (docker "bar"))
	(label "baz" (docker "baz"))
	(label "qux" (docker "qux"))
	(place (labelRule "exclusive" "foo" "bar") "baz" "qux")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "baz",
			Exclusive:   true,
			OtherLabel:  "foo",
		},
		db.Placement{
			TargetLabel: "baz",
			Exclusive:   true,
			OtherLabel:  "bar",
		},
		db.Placement{
			TargetLabel: "qux",
			Exclusive:   true,
			OtherLabel:  "foo",
		},
		db.Placement{
			TargetLabel: "qux",
			Exclusive:   true,
			OtherLabel:  "bar",
		},
	)

	// Machine placement
	spec = `(label "foo" (docker "foo"))
	(place (machineRule "on" (size "m4.large")) "foo")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "foo",
			Exclusive:   false,
			Size:        "m4.large",
		},
	)

	// Port placement
	spec = `(label "foo" (docker "foo"))
	(connect 80 "public" "foo")
	(connect 81 "public" "foo")`
	checkPlacement(spec,
		db.Placement{
			TargetLabel: "foo",
			Exclusive:   true,
			OtherLabel:  "foo",
		},
	)

	spec = `(label "foo" (docker "foo"))
                (label "bar" (docker "bar"))
                (label "baz" (docker "baz"))
                (connect 80 "public" "foo")
                (connect 80 "public" "bar")
		((lambda ()
			(connect 81 "public" "bar")
			(connect 81 "public" "baz")))`

	checkPlacement(spec,
		db.Placement{
			TargetLabel: "foo",
			Exclusive:   true,
			OtherLabel:  "foo",
		},

		db.Placement{
			TargetLabel: "bar",
			Exclusive:   true,
			OtherLabel:  "bar",
		},

		db.Placement{
			TargetLabel: "bar",
			Exclusive:   true,
			OtherLabel:  "foo",
		},

		db.Placement{
			TargetLabel: "baz",
			Exclusive:   true,
			OtherLabel:  "baz",
		},

		db.Placement{
			TargetLabel: "baz",
			Exclusive:   true,
			OtherLabel:  "bar",
		},
	)
}
