package scheduler

import (
	"container/heap"
	"fmt"
	"sort"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"
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
	conn.Txn(db.ContainerTable, db.EtcdTable, db.MinionTable,
		db.PlacementTable).Run(func(view db.Database) error {

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
		var valid []*db.Container
		for _, dbc := range m.containers {
			if validPlacement(ctx.constraints, *m, valid, dbc) {
				valid = append(valid, dbc)
				continue
			}
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

// Compute the peer labels map if it is nil, otherwise just return it
func computePeerLabels(peerLabels map[string]struct{}, peers []*db.Container,
	dbcID int) map[string]struct{} {

	if peerLabels != nil {
		return peerLabels
	}

	peerLabels = map[string]struct{}{}
	for _, peer := range peers {
		if peer.ID == dbcID {
			continue
		}

		for _, label := range peer.Labels {
			peerLabels[label] = struct{}{}
		}
	}
	return peerLabels
}

// Check that the placement is not violated by both directions of the constraint
func validExclusion(target, other string, cLabels, pLabels map[string]struct{}) bool {
	_, tcOK := cLabels[target]
	_, tpOK := pLabels[other]
	tValid := !tcOK || !tpOK

	_, ocOK := cLabels[other]
	_, opOK := pLabels[target]
	oValid := !ocOK || !opOK

	return tValid && oValid
}

func checkExclusionConstraint(constraint db.Placement, cLabels,
	pLabels map[string]struct{}) bool {

	if !constraint.Exclusive {
		// XXX: Inclusive OtherLabel is hard because we can't
		// make placement decisions without considering all the
		// containers on all of the minions.
		log.WithField("constraint", constraint).Warning(
			"Quilt currently does not support inclusive" +
				" label placement constraints")
		return true
	}

	return validExclusion(constraint.TargetLabel, constraint.OtherLabel,
		cLabels, pLabels)
}

func validPlacement(constraints []db.Placement, m minion, peers []*db.Container,
	dbc *db.Container) bool {

	cLabels := map[string]struct{}{}
	for _, label := range dbc.Labels {
		cLabels[label] = struct{}{}
	}

	var peerLabels map[string]struct{}
	for _, constraint := range constraints {
		if constraint.OtherLabel != "" {
			peerLabels = computePeerLabels(peerLabels, peers, dbc.ID)
			ok := checkExclusionConstraint(constraint, cLabels, peerLabels)
			if !ok {
				return false
			}
		}

		if _, ok := cLabels[constraint.TargetLabel]; !ok {
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
	case !util.StrSliceEqual(s[i].Command, s[j].Command):
		return fmt.Sprintf("%s", s[i].Command) < fmt.Sprintf("%s", s[j].Command)
	default:
		return s[i].StitchID < s[j].StitchID
	}
}

func (s dbcSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s dbcSlice) Len() int {
	return len(s)
}
