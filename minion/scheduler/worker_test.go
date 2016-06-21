package scheduler

import (
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/docker"
	"github.com/davecgh/go-spew/spew"
)

func TestRunWorker(t *testing.T) {
	t.Parallel()

	md, dk := docker.NewMock()
	conn := db.New()

	conn.Transact(func(view db.Database) error {
		container := view.InsertContainer()
		container.Image = "Image"
		container.Minion = "1.2.3.4"
		view.Commit(container)
		return nil
	})

	// Wrong Minion IP, should do nothing.
	runWorker(conn, dk, "5.6.7.8")
	dkcs, err := dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if len(dkcs) > 0 {
		t.Error(spew.Sprintf("Unexpected containers: %v", dkcs))
	}

	// Run with a list error, should do nothing.
	md.ListError = true
	runWorker(conn, dk, "1.2.3.4")
	md.ListError = false
	dkcs, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if len(dkcs) > 0 {
		t.Error(spew.Sprintf("Unexpected containers: %v", dkcs))
	}

	runWorker(conn, dk, "1.2.3.4")
	dkcs, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if len(dkcs) != 1 || dkcs[0].Image != "Image" {
		t.Error(spew.Sprintf("Unexpected containers: %v", dkcs))
	}
}

func TestSyncWorker(t *testing.T) {
	t.Parallel()

	md, dk := docker.NewMock()

	dbcs := []db.Container{
		{
			ID:      1,
			Image:   "Image1",
			Command: []string{"Cmd1"},
			Env:     map[string]string{"Env": "1"},
		},
	}

	md.StartError = true
	changed := syncWorker(dk, dbcs, nil)
	md.StartError = false
	if len(changed) > 0 {
		t.Error(spew.Sprintf("Expected no changed to to an error\n%v", changed))
	}

	changed = syncWorker(dk, dbcs, nil)

	dkcs, err := dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}

	if changed[0].DockerID != dkcs[0].ID {
		t.Error(spew.Sprintf("Incorrect DockerID: %v", changed))
	}

	dbcs[0].DockerID = dkcs[0].ID
	if !eq(changed, dbcs) {
		t.Error(expLog("Changed DB Containers", changed, dbcs))
	}

	dkcsDB := []db.Container{
		{
			ID:       1,
			DockerID: dkcs[0].ID,
			Image:    dkcs[0].Image,
			Command:  dkcs[0].Args,
			Env:      dkcs[0].Env,
		},
	}
	if !eq(dkcsDB, dbcs) {
		t.Error(expLog("Incorrect docker.List()", dkcsDB, dbcs))
	}

	dbcs[0].DockerID = ""
	changed = syncWorker(dk, dbcs, dkcs)

	newDkcs, err := dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if !eq(newDkcs, dkcs) {
		t.Error(expLog("Unexpected container change", newDkcs, dkcs))
	}

	dbcs[0].DockerID = dkcs[0].ID
	if !eq(changed, dbcs) {
		t.Error(expLog("Incorrect DB Containers", changed, dbcs))
	}

	// Atempt a failed remove
	md.RemoveError = true
	changed = syncWorker(dk, nil, dkcs)
	md.RemoveError = false
	if len(changed) > 0 {
		t.Error(spew.Sprintf("Expected no changed to to an error\n%v", changed))
	}
	newDkcs, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if !eq(newDkcs, dkcs) {
		t.Error(expLog("Unexpected container change", newDkcs, dkcs))
	}

	changed = syncWorker(dk, nil, dkcs)
	if len(changed) > 0 {
		t.Error(spew.Sprintf("Expected no changed to to an error\n%v", changed))
	}
	dkcs, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected err %v", err)
	}
	if len(dkcs) > 0 {
		t.Error(expLog("Unexpected containers", dkcs, nil))
	}
}

func TestSyncJoinScore(t *testing.T) {
	t.Parallel()

	dbc := db.Container{
		Image:    "Image",
		Command:  []string{"cmd"},
		Env:      map[string]string{"a": "b"},
		DockerID: "DockerID",
	}
	dkc := docker.Container{
		Image: dbc.Image,
		Args:  dbc.Command,
		Env:   dbc.Env,
		ID:    dbc.DockerID,
	}

	score := syncJoinScore(dbc, dkc)
	if score != 0 {
		t.Errorf("Unexpected score %d", score)
	}

	dbc.Image = "Image1"
	score = syncJoinScore(dbc, dkc)
	if score != -1 {
		t.Errorf("Unexpected score %d", score)
	}
	dbc.Image = dkc.Image

	dbc.Command = []string{"wrong"}
	score = syncJoinScore(dbc, dkc)
	if score != -1 {
		t.Errorf("Unexpected score %d", score)
	}
	dbc.Command = dkc.Args

	dbc.Env = map[string]string{"a": "wrong"}
	score = syncJoinScore(dbc, dkc)
	if score != -1 {
		t.Errorf("Unexpected score %d", score)
	}
	dbc.Env = dkc.Env

	dbc.DockerID = "2"
	score = syncJoinScore(dbc, dkc)
	if score != 1 {
		t.Errorf("Unexpected score %d", score)
	}
	dbc.DockerID = dkc.ID
}

func expLog(msg string, got, exp interface{}) string {
	return spew.Sprintf("%s\nGot: %s\nExp: %s\n", msg, got, exp)
}
