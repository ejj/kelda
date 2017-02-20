package scheduler

import (
	"sort"
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
)

func TestPlaceContainers(t *testing.T) {
	t.Parallel()
	conn := db.New()

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
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

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		placeContainers(view)
		return nil
	})

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		dbcs := view.SelectFromContainer(nil)
		assert.Len(t, dbcs, 1)
		assert.Equal(t, "1", dbcs[0].Minion)
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
			FloatingIP:  "xxx.xxx.xxx.xxx",
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
	assert.Equal(t, expMinions, ctx.minions)
	assert.Equal(t, placements, ctx.constraints)

	expUnassigned := []*db.Container{
		{
			ID:     1,
			Labels: []string{"1"},
			Minion: "",
		},
	}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := expUnassigned
	assert.Equal(t, expChanged, ctx.changed)
}

func TestCleanupLabelRule(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			ID:     1,
			Labels: []string{"1"},
			Minion: "1",
		},
		{
			ID:     2,
			Labels: []string{"1"},
			Minion: "1",
		},
		{
			ID:     3,
			Labels: []string{"1"},
			Minion: "1",
		},
		{
			ID:     4,
			Labels: []string{"1"},
			Minion: "1",
		},
		{
			ID:     5,
			Labels: []string{"2"},
			Minion: "1",
		},
		{
			ID:     6,
			Labels: []string{"3"},
			Minion: "2",
		},
		{
			ID:     7,
			Labels: []string{"1"},
			Minion: "2",
		},
	}

	minions := []db.Minion{
		{
			PrivateIP: "1",
			Role:      db.Worker,
		},
		{
			PrivateIP: "2",
			Role:      db.Worker,
		},
	}

	placements := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "1",
			OtherLabel:  "2",
		},
		{
			Exclusive:   true,
			TargetLabel: "1",
			OtherLabel:  "3",
		},
	}

	ctx := makeContext(minions, placements, containers)
	cleanupPlacements(ctx)

	expMinions := []*minion{
		{
			Minion: minions[0],
			containers: []*db.Container{
				&containers[0],
				&containers[1],
				&containers[2],
				&containers[3],
			},
		},
		{
			Minion: minions[1],
			containers: []*db.Container{
				&containers[5],
			},
		},
	}

	assert.Equal(t, expMinions, ctx.minions)
	assert.Equal(t, placements, ctx.constraints)

	expUnassigned := []*db.Container{
		&containers[4],
		&containers[6],
	}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := expUnassigned
	assert.Equal(t, expChanged, ctx.changed)
}

func TestPlaceUnassigned(t *testing.T) {
	t.Parallel()

	var exp []*db.Container
	ctx := makeContext(nil, nil, nil)
	placeUnassigned(ctx)
	assert.Equal(t, exp, ctx.changed)

	minions := []db.Minion{
		{
			PrivateIP:  "1",
			Region:     "Region1",
			Role:       db.Worker,
			FloatingIP: "xxx.xxx.xxx.xxx",
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

	assert.Equal(t, exp, ctx.changed)

	ctx = makeContext(minions, placements, containers)
	placeUnassigned(ctx)
	assert.Nil(t, ctx.changed)

	placements[0].Exclusive = false
	placements[0].Region = "Nowhere"
	containers[0].Minion = ""
	ctx = makeContext(minions, placements, containers)
	placeUnassigned(ctx)
	assert.Nil(t, ctx.changed)
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
	assert.Equal(t, placements, ctx.constraints)

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
	assert.Equal(t, expMinions, ctx.minions)

	expUnassigned := []*db.Container{&containers[0], &containers[2]}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := []*db.Container{&containers[2]}
	assert.Equal(t, expChanged, ctx.changed)
}

func TestValidPlacementTwoWay(t *testing.T) {
	t.Parallel()

	dbc := &db.Container{ID: 1, Labels: []string{"red"}}
	m := minion{
		db.Minion{
			PrivateIP: "1.2.3.4",
			Provider:  "Provider",
			Size:      "Size",
			Region:    "Region",
		},
		[]*db.Container{{ID: 2, Labels: []string{"blue"}}},
	}

	dbc1 := &db.Container{ID: 4, Labels: []string{"blue"}}
	m1 := minion{
		db.Minion{
			PrivateIP: "1.2.3.4",
			Provider:  "Provider",
			Size:      "Size",
			Region:    "Region",
		},
		[]*db.Container{{ID: 3, Labels: []string{"red"}}},
	}

	constraints := []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "blue",
			OtherLabel:  "red",
		},
	}

	testCases := []struct {
		dbc *db.Container
		m   minion
	}{
		{dbc, m},
		{dbc1, m1},
	}

	for _, testCase := range testCases {
		res := validPlacement(constraints, testCase.m, testCase.m.containers,
			testCase.dbc)
		if res {
			t.Fatalf("Succeeded with bad placement: %s on %s",
				testCase.dbc.Labels[0],
				testCase.m.containers[0].Labels[0])
		}
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
	res := validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "blue",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	var empty []*db.Container
	res = validPlacement(constraints, m, empty, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "yellow",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			OtherLabel:  "magenta",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			OtherLabel:  "yellow",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)
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

	res := validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Provider:    "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Provider:    "NotProvider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	// Region
	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Region:      "Region",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Region:      "Region",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Region:      "NoRegion",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	// Size
	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "Size",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   true,
			TargetLabel: "red",
			Size:        "Size",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:   false,
			TargetLabel: "red",
			Size:        "NoSize",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

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
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

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
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)
}

func TestSort(t *testing.T) {
	a := &db.Container{Image: "1", StitchID: "1"}
	b := &db.Container{Image: "1", StitchID: "2"}
	c := &db.Container{Image: "2", Command: []string{"1", "2"}}
	d := &db.Container{Image: "2", Command: []string{"3", "4"}}

	slice := []*db.Container{d, c, b, a}
	sort.Sort(dbcSlice(slice))
	assert.Equal(t, slice, []*db.Container{a, b, c, d})
}

func (m minion) String() string {
	return spew.Sprintf("(%s Containers: %s)", m.Minion, m.containers)
}
