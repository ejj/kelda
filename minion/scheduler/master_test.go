package scheduler

import (
	"reflect"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/davecgh/go-spew/spew"
)

var eq = reflect.DeepEqual

func TestPlaceContainers(t *testing.T) {
	t.Parallel()
	conn := db.New()

	conn.Transact(func(view db.Database) error {
		m := view.InsertMinion()
		m.PrivateIP = "1"
		m.Role = db.Worker
		view.Commit(m)

		e := view.InsertEtcd()
		e.Leader = true
		view.Commit(e)

		c := view.InsertContainer()
		view.Commit(c)
		return nil
	})

	conn.Transact(func(view db.Database) error {
		placeContainers(view)
		return nil
	})

	conn.Transact(func(view db.Database) error {
		dbcs := view.SelectFromContainer(nil)
		if len(dbcs) != 1 {
			t.Error(spew.Sprintf("len dbs: %v", dbcs))
		}

		if dbcs[0].Minion != "1" {
			t.Error(spew.Sprintf("minion: %v", dbcs))
		}
		return nil
	})
}

func TestCleanup(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			ID:     1,
			Labels: []string{"1"},
			Minion: "1",
		},
		{
			ID:     2,
			Labels: []string{"2"},
			Minion: "1",
		},
	}

	minions := []db.Minion{
		{
			PrivateIP: "1",
			Region:    "Region1",
			Role:      db.Worker,
		},
	}
	placements := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "1",
			Region:      "Region1",
		},
	}

	ctx := makeContext(minions, placements, containers)
	cleanupPlacements(ctx)

	expMinions := []*minion{
		{
			Minion:     minions[0],
			containers: []*db.Container{&containers[1]},
		},
	}
	if !eq(ctx.minions, expMinions) {
		t.Error(spew.Sprintf("\nMinions:  %v\nExpected: %v",
			ctx.minions, expMinions))
	}

	if !eq(ctx.constraints, placements) {
		t.Error(spew.Sprintf("\nConstraints: %v\nExpected:    %v",
			ctx.constraints, placements))
	}

	expUnassigned := []*db.Container{
		{
			ID:     1,
			Labels: []string{"1"},
			Minion: "",
		},
	}
	if !eq(ctx.unassigned, expUnassigned) {
		t.Error(spew.Sprintf("\nUnassigned: %v\nExpected:   %v",
			ctx.unassigned, expUnassigned))
	}

	expChanged := expUnassigned
	if !eq(ctx.changed, expChanged) {
		t.Error(spew.Sprintf("\nChanged:  %v\nExpected: %v",
			ctx.changed, expChanged))
	}
}

func TestPlaceUnassigned(t *testing.T) {
	t.Parallel()

	var exp []*db.Container
	ctx := makeContext(nil, nil, nil)
	placeUnassigned(ctx)
	if !eq(ctx.changed, exp) {
		t.Error(spew.Sprintf("\nChanged    %v\nExpChanged %v\n", ctx.changed,
			exp))
	}

	minions := []db.Minion{
		{
			PrivateIP: "1",
			Region:    "Region1",
			Role:      db.Worker,
		},
		{
			PrivateIP: "2",
			Region:    "Region2",
			Role:      db.Worker,
		},
		{
			PrivateIP: "3",
			Region:    "Region3",
			Role:      db.Worker,
		},
	}
	containers := []db.Container{
		{
			ID:     1,
			Labels: []string{"1"},
		},
		{
			ID:     2,
			Labels: []string{"2"},
		},
		{
			ID:     3,
			Labels: []string{"3"},
		},
	}
	placements := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "1",
			Region:      "Region1",
		},
	}

	ctx = makeContext(minions, placements, containers)
	placeUnassigned(ctx)

	exp = nil
	for _, dbc := range containers {
		copy := dbc
		exp = append(exp, &copy)
	}

	exp[0].Minion = "2"
	exp[1].Minion = "1"
	exp[2].Minion = "3"

	if !eq(ctx.changed, exp) {
		t.Error(spew.Sprintf("\nChanged    %v\nExpChanged %v\n", ctx.changed,
			exp))
	}

	ctx = makeContext(minions, placements, containers)
	placeUnassigned(ctx)
	exp = nil
	if !eq(ctx.changed, exp) {
		t.Error(spew.Sprintf("\nChanged    %v\nExpChanged %v\n", ctx.changed,
			exp))
	}

	placements[0].Exclusive = false
	placements[0].Region = "Nowhere"
	containers[0].Minion = ""
	ctx = makeContext(minions, placements, containers)
	placeUnassigned(ctx)
	exp = nil
	if !eq(ctx.changed, exp) {
		t.Error(spew.Sprintf("\nChanged    %v\nExpChanged %v\n", ctx.changed,
			exp))
	}
}

func TestMakeContext(t *testing.T) {
	t.Parallel()

	minions := []db.Minion{
		{
			ID:        1,
			PrivateIP: "1",
			Role:      db.Worker,
		},
		{
			ID:        2,
			PrivateIP: "2",
			Role:      db.Worker,
		},
		{
			ID:        3,
			PrivateIP: "3",
			Region:    "Region3",
		},
	}
	containers := []db.Container{
		{
			ID: 1,
		},
		{
			ID:     2,
			Minion: "1",
		},
		{
			ID:     3,
			Minion: "3",
		},
	}
	placements := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "1",
			Region:      "Region1",
		},
	}

	ctx := makeContext(minions, placements, containers)

	if !eq(ctx.constraints, placements) {
		t.Error(spew.Sprintf("\nConstraints: %v\nExpected:    %v",
			ctx.constraints, placements))
	}

	expMinions := []*minion{
		{
			Minion:     minions[0],
			containers: []*db.Container{&containers[1]},
		},
		{
			Minion:     minions[1],
			containers: nil,
		},
	}

	if !eq(ctx.minions, expMinions) {
		t.Error(spew.Sprintf("\nMinions:  %v\nExpected: %v",
			ctx.minions, expMinions))
	}

	expUnassigned := []*db.Container{&containers[0], &containers[2]}
	if !eq(ctx.unassigned, expUnassigned) {
		t.Error(spew.Sprintf("\nUnassigned: %v\nExpected:   %v",
			ctx.unassigned, expUnassigned))
	}

	expChanged := []*db.Container{&containers[2]}
	if !eq(ctx.changed, expChanged) {
		t.Error(spew.Sprintf("\nChanged:  %v\nExpected: %v",
			ctx.changed, expChanged))
	}
}

func TestValidPlacementLabel(t *testing.T) {
	t.Parallel()

	dbc := &db.Container{
		ID:     1,
		Labels: []string{"red"},
	}

	m := minion{}
	m.PrivateIP = "1.2.3.4"
	m.Provider = "Provider"
	m.Size = "Size"
	m.Region = "Region"
	m.containers = []*db.Container{
		dbc,
		{
			ID:     2,
			Labels: []string{"blue"},
		},
		{
			ID:     3,
			Labels: []string{"yellow", "orange"},
		},
	}

	constraints := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "blue", // Wrong target.
			OtherLabel:  "orange",
		},
	}
	res := validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v\nMinion %v\nContainer %v\nConstraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "blue",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v\nMinion %v\nContainer %v\nConstraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "yellow",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v\nMinion %v\nContainer %v\nConstraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "magenta",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v\nMinion %v\nContainer %v\nConstraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			OtherLabel:  "yellow",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v\nMinion %v\nContainer %v\nConstraints %v",
			res, m, dbc, constraints))
	}
}

func TestValidPlacementMachine(t *testing.T) {
	t.Parallel()

	var constraints []db.Placement

	dbc := &db.Container{}
	dbc.Labels = []string{"red"}

	m := minion{}
	m.PrivateIP = "1.2.3.4"
	m.Provider = "Provider"
	m.Size = "Size"
	m.Region = "Region"

	res := validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "NotProvider",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	// Region
	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Region:      "Region",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Region:      "Region",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Region:      "NoRegion",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	// Size
	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "Size",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Size:        "Size",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "NoSize",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	// Combination
	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "Size",
		},
		{
			Exclusive:   false,
			TargetLabel: "red",
			Region:      "Region",
		},
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if !res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "Size",
		},
		{
			Exclusive:   true,
			TargetLabel: "red",
			Region:      "Region",
		},
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, dbc)
	if res {
		t.Error(spew.Sprintf(
			"Unexpected %v. Minion %v, Container %v, Constraints %v",
			res, m, dbc, constraints))
	}
}

func (m minion) String() string {
	return spew.Sprintf("(%s Containers: %s)", m.Minion, m.containers)
}
