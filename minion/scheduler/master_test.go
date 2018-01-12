package scheduler

import (
	"sort"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/kelda/kelda/db"
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
			ID:       1,
			Hostname: "1",
			Minion:   "1",
		},
		{
			ID:       2,
			Hostname: "2",
			Minion:   "1",
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
			Exclusive:       true,
			TargetContainer: "1",
			Region:          "Region1",
			FloatingIP:      "xxx.xxx.xxx.xxx",
		},
	}

	ctx := makeContext(minions, placements, containers, nil)
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
			ID:       1,
			Hostname: "1",
			Minion:   "",
		},
	}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := expUnassigned
	assert.Equal(t, expChanged, ctx.changed)
}

func TestCleanupContainerRule(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			ID:       1,
			Hostname: "1",
			Minion:   "1",
		},
		{
			ID:       2,
			Hostname: "2",
			Minion:   "1",
		},
		{
			ID:       3,
			Hostname: "3",
			Minion:   "2",
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
			Exclusive:       true,
			TargetContainer: "1",
			OtherContainer:  "2",
		},
		{
			Exclusive:       true,
			TargetContainer: "1",
			OtherContainer:  "3",
		},
	}

	ctx := makeContext(minions, placements, containers, nil)
	cleanupPlacements(ctx)

	expMinions := []*minion{
		{
			Minion: minions[0],
			containers: []*db.Container{
				&containers[0],
			},
		},
		{
			Minion: minions[1],
			containers: []*db.Container{
				&containers[2],
			},
		},
	}

	assert.Equal(t, expMinions, ctx.minions)
	assert.Equal(t, placements, ctx.constraints)

	expUnassigned := []*db.Container{
		&containers[1],
	}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := expUnassigned
	assert.Equal(t, expChanged, ctx.changed)
}

func TestPlaceUnassigned(t *testing.T) {
	t.Parallel()

	var exp []*db.Container
	ctx := makeContext(nil, nil, nil, nil)
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
			ID:       1,
			Hostname: "1",
		},
		{
			ID:       2,
			Hostname: "2",
		},
		{
			ID:       3,
			Hostname: "3",
		},
	}
	placements := []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "1",
			Region:          "Region1",
		},
	}

	ctx = makeContext(minions, placements, containers, nil)
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

	ctx = makeContext(minions, placements, containers, nil)
	placeUnassigned(ctx)
	assert.Nil(t, ctx.changed)

	placements[0].Exclusive = false
	placements[0].Region = "Nowhere"
	containers[0].Minion = ""
	ctx = makeContext(minions, placements, containers, nil)
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
	images := []db.Image{
		{
			Name:       "foo",
			Dockerfile: "bar",
			DockerID:   "baz",
			Status:     db.Built,
		},
		{
			Name:       "qux",
			Dockerfile: "quuz",
			Status:     db.Building,
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
		// Container is scheduled with wrong DockerID.
		{
			ID:         4,
			Image:      "foo",
			Dockerfile: "bar",
			DockerID:   "change",
		},
		// Image not built yet.
		{
			ID:         5,
			Image:      "foo",
			Dockerfile: "baz",
			DockerID:   "baz",
		},
		// Image not built yet.
		{
			ID:         6,
			Image:      "qux",
			Dockerfile: "quuz",
		},
	}
	placements := []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "1",
			Region:          "Region1",
		},
	}

	ctx := makeContext(minions, placements, containers, images)
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

	expUnassigned := []*db.Container{&containers[0], &containers[2], &containers[3]}
	assert.Equal(t, expUnassigned, ctx.unassigned)

	expChanged := []*db.Container{&containers[2], &containers[3]}
	assert.Equal(t, expChanged, ctx.changed)
}

func TestValidPlacementTwoWay(t *testing.T) {
	t.Parallel()

	dbc := &db.Container{ID: 1, Hostname: "red"}
	m := minion{
		db.Minion{
			PrivateIP: "1.2.3.4",
			Provider:  "Provider",
			Size:      "Size",
			Region:    "Region",
		},
		[]*db.Container{{ID: 2, Hostname: "blue"}},
	}

	dbc1 := &db.Container{ID: 4, Hostname: "blue"}
	m1 := minion{
		db.Minion{
			PrivateIP: "1.2.3.4",
			Provider:  "Provider",
			Size:      "Size",
			Region:    "Region",
		},
		[]*db.Container{{ID: 3, Hostname: "red"}},
	}

	constraints := []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "blue",
			OtherContainer:  "red",
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
				testCase.dbc.Hostname,
				testCase.m.containers[0].Hostname)
		}
	}
}

func TestValidPlacementContainer(t *testing.T) {
	t.Parallel()

	dbc := &db.Container{
		ID:       1,
		Hostname: "red",
	}

	m := minion{}
	m.PrivateIP = "1.2.3.4"
	m.Provider = "Provider"
	m.Size = "Size"
	m.Region = "Region"
	m.containers = []*db.Container{
		dbc,
		{
			ID:       2,
			Hostname: "blue",
		},
		{
			ID:       3,
			Hostname: "yellow",
		},
	}

	constraints := []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "blue", // Wrong target.
			OtherContainer:  "orange",
		},
	}
	res := validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			OtherContainer:  "blue",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	var empty []*db.Container
	res = validPlacement(constraints, m, empty, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			OtherContainer:  "yellow",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			OtherContainer:  "magenta",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			OtherContainer:  "yellow",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)
}

func TestValidPlacementMachine(t *testing.T) {
	t.Parallel()

	var constraints []db.Placement

	dbc := &db.Container{
		Hostname: "red",
	}

	m := minion{}
	m.PrivateIP = "1.2.3.4"
	m.Provider = "Provider"
	m.Size = "Size"
	m.Region = "Region"

	res := validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Provider:        "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			Provider:        "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Provider:        "NotProvider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	// Region
	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Region:          "Region",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			Region:          "Region",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Region:          "NoRegion",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	// Size
	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Size:            "Size",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       true,
			TargetContainer: "red",
			Size:            "Size",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Size:            "NoSize",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)

	// Combination
	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Size:            "Size",
		},
		{
			Exclusive:       false,
			TargetContainer: "red",
			Region:          "Region",
		},
		{
			Exclusive:       false,
			TargetContainer: "red",
			Provider:        "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.True(t, res)

	constraints = []db.Placement{
		{
			Exclusive:       false,
			TargetContainer: "red",
			Size:            "Size",
		},
		{
			Exclusive:       true,
			TargetContainer: "red",
			Region:          "Region",
		},
		{
			Exclusive:       false,
			TargetContainer: "red",
			Provider:        "Provider",
		},
	}
	res = validPlacement(constraints, m, m.containers, dbc)
	assert.False(t, res)
}

func TestSort(t *testing.T) {
	a := &db.Container{Image: "1", BlueprintID: "1"}
	b := &db.Container{Image: "1", BlueprintID: "2"}
	c := &db.Container{Image: "2", Command: []string{"1", "2"}}
	d := &db.Container{Image: "2", Command: []string{"3", "4"}}

	slice := []*db.Container{d, c, b, a}
	sort.Sort(dbcSlice(slice))
	assert.Equal(t, slice, []*db.Container{a, b, c, d})
}

func (m minion) String() string {
	return spew.Sprintf("(%s Containers: %s)", m.Minion, m.containers)
}
