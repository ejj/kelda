package scheduler

import (
	"container/heap"

	"github.com/NetSys/quilt/db"
	log "github.com/Sirupsen/logrus"
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

func runMaster(conn db.Conn) {
	conn.Transact(func(view db.Database) error {
		if view.EtcdLeader() {
			placeContainers(view)
		}
		return nil
	})
}

func placeContainers(view db.Database) {
	constraints := view.SelectFromPlacement(nil)
	containers := view.SelectFromContainer(nil)
	minions := view.SelectFromMinion(nil)

	ctx := makeContext(minions, constraints, containers)
	cleanupPlacements(ctx)
	placeUnassigned(ctx)

	for _, change := range ctx.changed {
		view.Commit(*change)
	}
}

// Unassign all containers that are placed incorrectly.
func cleanupPlacements(ctx *context) {
	for _, m := range ctx.minions {
		for i, dbc := range m.containers {
			if validPlacement(ctx.constraints, *m, dbc) {
				continue
			}
			dbc.Minion = ""
			m.containers = append(m.containers[:i], m.containers[i+1:]...)
			ctx.unassigned = append(ctx.unassigned, dbc)
			ctx.changed = append(ctx.changed, dbc)
		}
	}
}

func placeUnassigned(ctx *context) {
	minions := minionHeap(ctx.minions)
	heap.Init(&minions)

Outer:
	for _, dbc := range ctx.unassigned {
		for i, minion := range minions {
			if validPlacement(ctx.constraints, *minion, dbc) {
				dbc.Minion = minion.PrivateIP
				ctx.changed = append(ctx.changed, dbc)
				minion.containers = append(minion.containers, dbc)
				heap.Fix(&minions, i)
				log.WithField("container", dbc).Info("Placed container.")
				continue Outer
			}
		}

		log.WithField("container", dbc).Warning("Failed to place container.")
	}
}

func validPlacement(constraints []db.Placement, m minion, dbc *db.Container) bool {
	cLabels := map[string]struct{}{}
	for _, label := range dbc.Labels {
		cLabels[label] = struct{}{}
	}

	var peerLabels map[string]struct{}
	for _, constraint := range constraints {
		if _, ok := cLabels[constraint.TargetLabel]; !ok {
			continue
		}

		if constraint.OtherLabel != "" {
			// Initialize the peerLabels only if we need it.
			if peerLabels == nil {
				peerLabels = map[string]struct{}{}
				for _, peer := range m.containers {
					if peer.ID == dbc.ID {
						continue
					}

					for _, label := range peer.Labels {
						peerLabels[label] = struct{}{}
					}
				}
			}

			if constraint.Exclusive {
				if _, ok := peerLabels[constraint.OtherLabel]; ok {
					return false
				}
			} else {
				// XXX: Inclusive OtherLabel is hard because we can't
				// make placement decisions without considering all the
				// containers on all of the minions.
				log.WithField("constraint", constraint).Warning(
					"Quilt currently does not support inclusive" +
						" label placement constraints")
			}
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
	}

	return true
}

func makeContext(minions []db.Minion, constraints []db.Placement,
	containers []db.Container) *context {

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

	for i := range containers {
		dbc := &containers[i]
		minion := ipMinion[dbc.Minion]
		if minion == nil && dbc.Minion != "" {
			dbc.Minion = ""
			ctx.changed = append(ctx.changed, dbc)
		}

		if dbc.Minion == "" {
			ctx.unassigned = append(ctx.unassigned, dbc)
			continue
		}

		minion.containers = append(minion.containers, dbc)
	}

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
