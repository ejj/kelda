package minion

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util/str"
	"github.com/stretchr/testify/assert"
)

const testImage = "alpine"

func TestContainerTxn(t *testing.T) {
	conn := db.New()
	trigg := conn.Trigger(db.ContainerTable).C

	testContainerTxn(t, conn, blueprint.Blueprint{})
	assert.False(t, fired(trigg))

	bp := blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "f133411ac23f45342a7b8b89bbe5e9efd0e711e5",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"tail"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "a",
				Hostnames: []string{
					"foo",
				},
			},
		},
	}
	testContainerTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, bp)
	assert.False(t, fired(trigg))

	testContainerTxn(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "f133411ac23f45342a7b8b89bbe5e9efd0e711e5",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"tail"},
			},
			{
				Hostname: "bar",
				ID:       "6e24c8cbeb63dbffcc82730d01b439e2f5085f59",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"tail"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "b",
				Hostnames: []string{
					"foo",
				},
			},
			{
				Name: "a",
				Hostnames: []string{
					"foo",
					"bar",
				},
			},
		},
	})
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "0b8a2ed7d14d78a388375025223b05d072bbaec3",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"cat"},
			},
			{
				Hostname: "bar",
				ID:       "f133411ac23f45342a7b8b89bbe5e9efd0e711e5",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"tail"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "b",
				Hostnames: []string{
					"foo",
				},
			},
			{
				Name: "a",
				Hostnames: []string{
					"foo",
					"bar",
				},
			},
		},
	})
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "7a6244b8d2bfa10ee2fcbe6836a0519e116aee31",
				Image:    blueprint.Image{Name: "ubuntu"},
				Command:  []string{"cat"},
			},
			{
				Hostname: "bar",
				ID:       "f133411ac23f45342a7b8b89bbe5e9efd0e711e5",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"tail"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "b",
				Hostnames: []string{
					"foo",
				},
			},
			{
				Name: "a",
				Hostnames: []string{
					"foo",
					"bar",
				},
			},
		},
	})
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "0b8a2ed7d14d78a388375025223b05d072bbaec3",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"cat"},
			},
			{
				Hostname: "bar",
				ID:       "d1c9f501efd7a348e54388358c5fe29690fb147d",
				Image:    blueprint.Image{Name: "alpine"},
				Command:  []string{"cat"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "a",
				Hostnames: []string{
					"foo",
					"bar",
				},
			},
		},
	})
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "018e4ee517d85640d9bf0adb4579d2ac9bd358af",
				Image:    blueprint.Image{Name: "alpine"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "a",
				Hostnames: []string{
					"foo",
				},
			},
		},
	})
	assert.True(t, fired(trigg))

	bp = blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "foo",
				ID:       "018e4ee517d85640d9bf0adb4579d2ac9bd358af",
				Image:    blueprint.Image{Name: "alpine"},
			},
			{
				Hostname: "bar",
				ID:       "ac4693f0b7fc17aa0e885aa03dc8f7cd6017f496",
				Image:    blueprint.Image{Name: "alpine"},
			},
		},
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name: "b",
				Hostnames: []string{
					"foo",
				},
			},
			{
				Name: "c",
				Hostnames: []string{
					"bar",
				},
			},
			{
				Name: "a",
				Hostnames: []string{
					"foo",
					"bar",
				},
			},
		},
	}
	testContainerTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testContainerTxn(t, conn, bp)
	assert.False(t, fired(trigg))
}

func testContainerTxn(t *testing.T, conn db.Conn, bp blueprint.Blueprint) {
	var containers []db.Container
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		updatePolicy(view, bp.String())
		containers = view.SelectFromContainer(nil)
		return nil
	})

	for _, e := range queryContainers(bp) {
		found := false
		for i, c := range containers {
			if e.BlueprintID == c.BlueprintID {
				containers = append(containers[:i], containers[i+1:]...)
				found = true
				break
			}
		}

		assert.True(t, found)
	}

	assert.Empty(t, containers)
}

func TestConnectionTxn(t *testing.T) {
	conn := db.New()
	trigg := conn.Trigger(db.ConnectionTable).C

	testConnectionTxn(t, conn, blueprint.Blueprint{})
	assert.False(t, fired(trigg))

	bp := blueprint.Blueprint{
		Connections: []blueprint.Connection{
			{From: []string{"a"}, To: []string{"a"},
				MinPort: 80, MaxPort: 80}}}
	testConnectionTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testConnectionTxn(t, conn, bp)
	assert.False(t, fired(trigg))

	bp.Connections = []blueprint.Connection{
		{From: []string{"a"}, To: []string{"a"}, MinPort: 90, MaxPort: 90},
	}
	testConnectionTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testConnectionTxn(t, conn, bp)
	assert.False(t, fired(trigg))

	bp.Connections = []blueprint.Connection{
		{From: []string{"b"}, To: []string{"a"}, MinPort: 90, MaxPort: 90},
		{From: []string{"b"}, To: []string{"c"}, MinPort: 90, MaxPort: 90},
		{From: []string{"b"}, To: []string{"a"}, MinPort: 100, MaxPort: 100},
		{From: []string{"c"}, To: []string{"a"}, MinPort: 101, MaxPort: 101},
	}
	testConnectionTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testConnectionTxn(t, conn, bp)
	assert.False(t, fired(trigg))

	bp.Connections = nil
	testConnectionTxn(t, conn, bp)
	assert.True(t, fired(trigg))

	testConnectionTxn(t, conn, bp)
	assert.False(t, fired(trigg))
}

func testConnectionTxn(t *testing.T, conn db.Conn, bp blueprint.Blueprint) {
	var connections []db.Connection
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		updatePolicy(view, bp.String())
		connections = view.SelectFromConnection(nil)
		return nil
	})

	exp := bp.Connections
	for _, e := range exp {
		found := false
		for i, c := range connections {
			if str.SliceEq(e.From, c.From) && str.SliceEq(e.To, c.To) &&
				e.MinPort == c.MinPort && e.MaxPort == c.MaxPort {
				connections = append(
					connections[:i], connections[i+1:]...)
				found = true
				break
			}
		}

		assert.True(t, found)
	}

	assert.Empty(t, connections)
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
	checkPlacement := func(bp blueprint.Blueprint, exp ...db.Placement) {
		var actual db.PlacementSlice
		conn.Txn(db.AllTables...).Run(func(view db.Database) error {
			updatePolicy(view, bp.String())
			actual = db.PlacementSlice(view.SelectFromPlacement(nil))
			return nil
		})

		key := func(plcmIntf interface{}) interface{} {
			plcm := plcmIntf.(db.Placement)
			plcm.ID = 0 // Ignore the Database ID.

			// If it's a container constraint, the order of TargetContainer
			// and OtherContainer doesn't matter. Therefore, we sort the
			// containers IDs so that the assignment is consistent.
			if plcm.OtherContainer != "" {
				ids := []string{plcm.TargetContainer, plcm.OtherContainer}
				sort.Strings(ids)
				plcm.TargetContainer = ids[0]
				plcm.OtherContainer = ids[1]
			}
			return plcm
		}
		_, missing, extra := join.HashJoin(db.PlacementSlice(exp), actual,
			key, key)
		assert.Empty(t, missing)
		assert.Empty(t, extra)
	}

	fooHostname := "foo"
	barHostname := "bar"
	bazHostname := "baz"
	fooID := "fooID"
	barID := "barID"
	bazID := "bazID"
	bp := blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: fooHostname,
				ID:       fooID,
				Image:    blueprint.Image{Name: "foo"},
			},
			{
				Hostname: barHostname,
				ID:       barID,
				Image:    blueprint.Image{Name: "bar"},
			},
			{
				Hostname: bazHostname,
				ID:       bazID,
				Image:    blueprint.Image{Name: "baz"},
			},
		},
	}

	// Machine placement
	bp.Placements = []blueprint.Placement{
		{TargetContainerID: "foo", Exclusive: false, Size: "m4.large"},
	}
	checkPlacement(bp,
		db.Placement{
			TargetContainer: "foo",
			Exclusive:       false,
			Size:            "m4.large",
		},
	)

	// Port placement
	bp.Placements = nil
	bp.Connections = []blueprint.Connection{
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{fooHostname}, MinPort: 80, MaxPort: 80},
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{fooHostname}, MinPort: 81, MaxPort: 81},
	}
	checkPlacement(bp)

	bp.Connections = []blueprint.Connection{
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{fooHostname}, MinPort: 80, MaxPort: 80},
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{barHostname}, MinPort: 80, MaxPort: 80},
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{barHostname}, MinPort: 81, MaxPort: 81},
		{From: []string{blueprint.PublicInternetLabel},
			To: []string{bazHostname}, MinPort: 81, MaxPort: 81},
	}
	checkPlacement(bp,
		db.Placement{
			TargetContainer: fooID,
			OtherContainer:  barID,
			Exclusive:       true,
		},
		db.Placement{
			TargetContainer: barID,
			OtherContainer:  bazID,
			Exclusive:       true,
		},
	)
}

func checkImage(t *testing.T, conn db.Conn, bp blueprint.Blueprint, exp ...db.Image) {
	var images []db.Image
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		updatePolicy(view, bp.String())
		images = view.SelectFromImage(nil)
		return nil
	})

	key := func(intf interface{}) interface{} {
		im := intf.(db.Image)
		im.ID = 0
		return im
	}
	_, lonelyLeft, lonelyRight := join.HashJoin(
		db.ImageSlice(images), db.ImageSlice(exp), key, key)
	assert.Empty(t, lonelyLeft, "unexpected images")
	assert.Empty(t, lonelyRight, "missing images")
}

func TestImageTxn(t *testing.T) {
	t.Parallel()

	// Regular image that isn't built by Kelda.
	checkImage(t, db.New(), blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				ID:    "475c40d6070969839ba0f88f7a9bd0cc7936aa30",
				Image: blueprint.Image{Name: "image"},
			},
		},
	})

	conn := db.New()
	checkImage(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				ID:    "96189e4ea36c80171fd842ccc4c3438d06061991",
				Image: blueprint.Image{Name: "a", Dockerfile: "1"},
			},
			{
				ID:    "c51d206a1414f1fadf5020e5db35feee91410f79",
				Image: blueprint.Image{Name: "a", Dockerfile: "1"},
			},
			{
				ID:    "ede1e03efba48e66be3e51aabe03ec77d9f9def9",
				Image: blueprint.Image{Name: "b", Dockerfile: "1"},
			},
			{
				ID:    "133c61c61ef4b49ea26717efe0f0468d455fd317",
				Image: blueprint.Image{Name: "c"},
			},
		},
	},
		db.Image{
			Name:       "a",
			Dockerfile: "1",
		},
		db.Image{
			Name:       "b",
			Dockerfile: "1",
		},
	)

	// Ensure existing images are preserved.
	conn.Txn(db.ImageTable).Run(func(view db.Database) error {
		img := view.SelectFromImage(func(img db.Image) bool {
			return img.Name == "a"
		})[0]
		img.DockerID = "id"
		view.Commit(img)
		return nil
	})
	checkImage(t, conn, blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				ID:    "96189e4ea36c80171fd842ccc4c3438d06061991",
				Image: blueprint.Image{Name: "a", Dockerfile: "1"},
			},
			{
				ID:    "18c2c81fb48a2a481af58ba5ad6da0e2b244f060",
				Image: blueprint.Image{Name: "b", Dockerfile: "2"},
			},
		},
	},
		db.Image{
			Name:       "a",
			Dockerfile: "1",
			DockerID:   "id",
		},
		db.Image{
			Name:       "b",
			Dockerfile: "2",
		},
	)
}

func checkLoadBalancer(t *testing.T, conn db.Conn, bp blueprint.Blueprint,
	exp ...db.LoadBalancer) {
	var loadBalancers []db.LoadBalancer
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		updatePolicy(view, bp.String())
		loadBalancers = view.SelectFromLoadBalancer(nil)
		return nil
	})

	key := func(intf interface{}) interface{} {
		lb := intf.(db.LoadBalancer)
		return struct {
			Name, IP, Hostnames string
		}{
			lb.Name, lb.IP, fmt.Sprintf("%+v", lb.Hostnames),
		}
	}
	_, lonelyLeft, lonelyRight := join.HashJoin(
		db.LoadBalancerSlice(loadBalancers), db.LoadBalancerSlice(exp), key, key)
	assert.Empty(t, lonelyLeft, "unexpected load balancers")
	assert.Empty(t, lonelyRight, "missing load balancers")
}

func TestLoadBalancerTxn(t *testing.T) {
	t.Parallel()
	conn := db.New()

	loadBalancerA := "loadBalancerA"
	loadBalancerAIP := "8.8.8.8"
	hostnamesA := []string{"a", "aa"}

	// Insert a load balancer into an empty db.
	checkLoadBalancer(t, conn, blueprint.Blueprint{
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name:      loadBalancerA,
				Hostnames: hostnamesA,
			},
		},
	}, db.LoadBalancer{
		Name:      loadBalancerA,
		Hostnames: hostnamesA,
	})

	// Simulate allocating an IP to the load balancer. Ensure it doesn't get
	// overwritten in the join.
	conn.Txn(db.LoadBalancerTable).Run(func(view db.Database) error {
		lb := view.SelectFromLoadBalancer(func(lb db.LoadBalancer) bool {
			return lb.Name == loadBalancerA
		})[0]
		lb.IP = loadBalancerAIP
		view.Commit(lb)
		return nil
	})

	hostnamesANew := []string{"a", "aa", "aaa"}
	checkLoadBalancer(t, conn, blueprint.Blueprint{
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name:      loadBalancerA,
				Hostnames: hostnamesANew,
			},
		},
	}, db.LoadBalancer{
		Name:      loadBalancerA,
		Hostnames: hostnamesANew,
		IP:        loadBalancerAIP,
	})

	// Change the deployment so that the current load balancer is removed, and a
	// different one is inserted.
	loadBalancerB := "loadBalancerB"
	hostnamesB := []string{"b", "bb"}
	checkLoadBalancer(t, conn, blueprint.Blueprint{
		LoadBalancers: []blueprint.LoadBalancer{
			{
				Name:      loadBalancerB,
				Hostnames: hostnamesB,
			},
		},
	}, db.LoadBalancer{
		Name:      loadBalancerB,
		Hostnames: hostnamesB,
	})
}
