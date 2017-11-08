package scheduler

import (
	"container/heap"
	"fmt"
	"sort"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util/str"
	log "github.com/sirupsen/logrus"
)

type minion struct {
	db.Minion
	containers []*db.Container
}

type context struct {
	minions     []*minion
	constraints []db.Placement
	unassigned  []*db.Container
	changed     []*db.Container
}

// isMasterReady waits for there to be at least one worker in the database so
// the scheduling code can place containers.
func isMasterReady(conn db.Conn) bool {
	workers := conn.SelectFromMinion(func(m db.Minion) bool {
		return m.Role == db.Worker
	})
	return len(workers) > 0
}

func runMaster(conn db.Conn) {
	if !conn.EtcdLeader() {
		return
	}

	conn.Txn(db.ContainerTable, db.MinionTable, db.ImageTable,
		db.PlacementTable).Run(func(view db.Database) error {
		placeContainers(view)
		return nil
	})
}

func placeContainers(view db.Database) {
	constraints := view.SelectFromPlacement(nil)
	containers := view.SelectFromContainer(nil)
	minions := view.SelectFromMinion(nil)
	images := view.SelectFromImage(nil)

	ctx := makeContext(minions, constraints, containers, images)
	cleanupPlacements(ctx)
	placeUnassigned(ctx)

	for _, change := range ctx.changed {
		view.Commit(*change)
	}
}

// Unassign all containers that are placed incorrectly.
func cleanupPlacements(ctx *context) {
	for _, m := range ctx.minions {
		var valid []*db.Container
		for _, dbc := range m.containers {
			if validPlacement(ctx.constraints, *m, valid, dbc) {
				valid = append(valid, dbc)
				continue
			}
			c.Inc("Reschedule Container")
			dbc.Minion = ""
			ctx.unassigned = append(ctx.unassigned, dbc)
			ctx.changed = append(ctx.changed, dbc)
		}
		m.containers = valid
	}
}

func placeUnassigned(ctx *context) {
	minions := minionHeap(ctx.minions)
	heap.Init(&minions)

Outer:
	for _, dbc := range ctx.unassigned {
		for i, m := range minions {
			if validPlacement(ctx.constraints, *m, m.containers, dbc) {
				c.Inc("Place Container")
				dbc.Minion = m.PrivateIP
				ctx.changed = append(ctx.changed, dbc)
				m.containers = append(m.containers, dbc)
				heap.Fix(&minions, i)
				log.WithField("container", dbc).Info("Placed container.")
				continue Outer
			}
		}

		log.WithField("container", dbc).Warning("Failed to place container.")
	}
}

func canBeColocated(constraint db.Placement, toPlace db.Container,
	peers []*db.Container) bool {
	if !constraint.Exclusive {
		// XXX: Inclusive OtherContainer is hard because we can't
		// make placement decisions without considering all the
		// containers on all of the minions.
		log.WithField("constraint", constraint).Warning(
			"Kelda currently does not support inclusive" +
				" container placement constraints")
		return true
	}

	for _, p := range peers {
		toPlaceIsTarget := toPlace.BlueprintID == constraint.TargetContainer
		peerIsTarget := p.BlueprintID == constraint.TargetContainer
		toPlaceIsOther := toPlace.BlueprintID == constraint.OtherContainer
		peerIsOther := p.BlueprintID == constraint.OtherContainer
		if (toPlaceIsTarget && peerIsOther) || (peerIsTarget && toPlaceIsOther) {
			return false
		}
	}
	return true
}

func validPlacement(constraints []db.Placement, m minion, peers []*db.Container,
	dbc *db.Container) bool {

	for _, constraint := range constraints {
		if constraint.OtherContainer != "" {
			if !canBeColocated(constraint, *dbc, peers) {
				return false
			}
		}

		if dbc.BlueprintID != constraint.TargetContainer {
			continue
		}

		if constraint.Provider != "" {
			on := constraint.Provider == m.Provider
			if constraint.Exclusive == on {
				return false
			}
		}

		if constraint.Region != "" {
			on := constraint.Region == m.Region
			if constraint.Exclusive == on {
				return false
			}
		}

		if constraint.Size != "" {
			on := constraint.Size == m.Size
			if constraint.Exclusive == on {
				return false
			}
		}

		if constraint.FloatingIP != "" {
			on := constraint.FloatingIP == m.FloatingIP
			if constraint.Exclusive == on {
				return false
			}
		}
	}

	return true
}

func makeContext(minions []db.Minion, constraints []db.Placement,
	containers []db.Container, images []db.Image) *context {

	ctx := context{}
	ctx.constraints = constraints

	ipMinion := map[string]*minion{}
	for _, dbm := range minions {
		if dbm.Role != db.Worker || dbm.PrivateIP == "" {
			continue
		}

		m := minion{dbm, nil}
		ctx.minions = append(ctx.minions, &m)
		ipMinion[m.PrivateIP] = &m
	}

	builtImages := map[db.Image]db.Image{}
	for _, img := range images {
		if img.Status == db.Built {
			builtImages[db.Image{
				Name:       img.Name,
				Dockerfile: img.Dockerfile,
			}] = img
		}
	}

	for i := range containers {
		dbc := &containers[i]
		minion := ipMinion[dbc.Minion]
		if minion == nil && dbc.Minion != "" {
			dbc.Minion = ""
			ctx.changed = append(ctx.changed, dbc)
		}

		// If the container is built by Kelda, only schedule it if the image
		// has been built.
		if dbc.Dockerfile != "" {
			img, ok := builtImages[db.Image{
				Name:       dbc.Image,
				Dockerfile: dbc.Dockerfile,
			}]
			if !ok {
				continue
			}
			if dbc.ImageID != img.DockerID {
				dbc.ImageID = img.DockerID
				ctx.changed = append(ctx.changed, dbc)
			}
		}

		if dbc.Minion == "" {
			ctx.unassigned = append(ctx.unassigned, dbc)
			continue
		}

		minion.containers = append(minion.containers, dbc)
	}

	// XXX: We sort containers based on their image and command in an effort to
	// encourage the scheduler to spread them out.  This is somewhat of a hack -- we
	// need a more clever scheduler at some point.
	sort.Sort(dbcSlice(ctx.unassigned))

	return &ctx
}

// Minion Heap.  Minions are sorted based on the number of containers scheduled on them
// with fewer containers being higher priority.
type minionHeap []*minion

func (mh minionHeap) Len() int      { return len(mh) }
func (mh minionHeap) Swap(i, j int) { mh[i], mh[j] = mh[j], mh[i] }

// We don't actually use Push and Pop and the moment.  See Heap docs if needed later.
func (mh *minionHeap) Push(x interface{}) { panic("Not Reached") }
func (mh *minionHeap) Pop() interface{}   { panic("Not Reached") }

func (mh minionHeap) Less(i, j int) bool {
	return len(mh[i].containers) < len(mh[j].containers)
}

type dbcSlice []*db.Container

func (s dbcSlice) Less(i, j int) bool {
	switch {
	case s[i].Image != s[j].Image:
		return s[i].Image < s[j].Image
	case !str.SliceEq(s[i].Command, s[j].Command):
		return fmt.Sprintf("%s", s[i].Command) < fmt.Sprintf("%s", s[j].Command)
	default:
		return s[i].BlueprintID < s[j].BlueprintID
	}
}

func (s dbcSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s dbcSlice) Len() int {
	return len(s)
}
