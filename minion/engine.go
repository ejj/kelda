package main

import (
	"sort"
	"strings"
	"text/scanner"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/stitch"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
)

func updatePolicy(view db.Database, role db.Role, spec string) {
	var sc scanner.Scanner
	compiled, err := stitch.New(*sc.Init(strings.NewReader(spec)), "", false)
	if err != nil {
		log.WithError(err).Warn("Invalid spec.")
		return
	}

	updateConnections(view, compiled)
	if role == db.Master {
		// This must happen after `updateConnections` because we generate
		// placement rules based on whether there are incoming connections from
		// public internet.
		updatePlacements(view, compiled)

		// The container table is aspirational -- it's the set of containers that
		// should exist.  In the workers, however, the container table is just
		// what's running locally.  That's why we only sync the database
		// containers on the master.
		updateContainers(view, compiled)
	}
}

func updatePlacements(view db.Database, spec stitch.Stitch) {
	var placements db.PlacementSlice
	for _, sp := range spec.QueryPlacements() {
		placements = append(placements, db.Placement{
			TargetLabel: sp.TargetLabel,
			Exclusive:   sp.Exclusive,
			OtherLabel:  sp.OtherLabel,
			Provider:    sp.Provider,
			Size:        sp.Size,
			Region:      sp.Region,
		})
	}

	key := func(val interface{}) interface{} {
		p := val.(db.Placement)
		p.ID = 0
		return p
	}

	dbPlacements := db.PlacementSlice(view.SelectFromPlacement(nil))
	_, addSet, removeSet := join.HashJoin(placements, dbPlacements, key, key)

	for _, toAddIntf := range addSet {
		toAdd := toAddIntf.(db.Placement)

		id := view.InsertPlacement().ID
		toAdd.ID = id
		view.Commit(toAdd)
	}

	for _, toRemove := range removeSet {
		view.Remove(toRemove.(db.Placement))
	}
}

func updateConnections(view db.Database, spec stitch.Stitch) {
	scs, vcs := stitch.ConnectionSlice(spec.QueryConnections()),
		view.SelectFromConnection(nil)

	dbcKey := func(val interface{}) interface{} {
		c := val.(db.Connection)
		return stitch.Connection{
			From:    c.From,
			To:      c.To,
			MinPort: c.MinPort,
			MaxPort: c.MaxPort,
		}
	}

	pairs, stitchs, dbcs := join.HashJoin(scs, db.ConnectionSlice(vcs), nil, dbcKey)

	for _, dbc := range dbcs {
		view.Remove(dbc.(db.Connection))
	}

	for _, stitchc := range stitchs {
		pairs = append(pairs, join.Pair{L: stitchc, R: view.InsertConnection()})
	}

	for _, pair := range pairs {
		stitchc := pair.L.(stitch.Connection)
		dbc := pair.R.(db.Connection)

		dbc.From = stitchc.From
		dbc.To = stitchc.To
		dbc.MinPort = stitchc.MinPort
		dbc.MaxPort = stitchc.MaxPort
		view.Commit(dbc)
	}
}

func queryContainers(spec stitch.Stitch) []db.Container {
	containers := map[int]*db.Container{}
	for _, c := range spec.QueryContainers() {
		containers[c.ID] = &db.Container{
			StitchID: c.ID,
			Command:  c.Command,
			Image:    c.Image,
			Env:      c.Env,
		}
	}

	for label, ids := range spec.QueryLabels() {
		for _, id := range ids {
			containers[id].Labels = append(containers[id].Labels, label)
		}
	}

	var ret []db.Container
	for _, c := range containers {
		ret = append(ret, *c)
	}

	return ret
}

func updateContainers(view db.Database, spec stitch.Stitch) {
	score := func(l, r interface{}) int {
		left := l.(db.Container)
		right := r.(db.Container)

		if left.Image != right.Image ||
			!util.StrSliceEqual(left.Command, right.Command) ||
			!util.StrStrMapEqual(left.Env, right.Env) {
			return -1
		}

		score := util.EditDistance(left.Labels, right.Labels)
		if left.StitchID != right.StitchID {
			score++
		}
		return score
	}

	pairs, news, dbcs := join.Join(queryContainers(spec),
		view.SelectFromContainer(nil), score)

	for _, dbc := range dbcs {
		view.Remove(dbc.(db.Container))
	}

	for _, new := range news {
		pairs = append(pairs, join.Pair{L: new, R: view.InsertContainer()})
	}

	for _, pair := range pairs {
		newc := pair.L.(db.Container)
		dbc := pair.R.(db.Container)

		// By sorting the labels we prevent the database from getting confused
		// when their order is non determinisitic.
		dbc.Labels = newc.Labels
		sort.Sort(sort.StringSlice(dbc.Labels))

		dbc.Command = newc.Command
		dbc.Image = newc.Image
		dbc.Env = newc.Env
		dbc.StitchID = newc.StitchID
		view.Commit(dbc)
	}
}
