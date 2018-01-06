package etcd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
)

func TestRunContainerOnce(t *testing.T) {
	t.Parallel()

	store := newTestMock()
	conn := db.New()

	err := runContainerOnce(conn, store)
	assert.Error(t, err)

	err = store.Set(containerPath, "", 0)
	assert.NoError(t, err)

	// Setup the database as if it were the leader, and had a single container.
	// runContainerOnce should write the container into etcd.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		self := view.InsertMinion()
		self.Self = true
		self.Role = db.Master
		view.Commit(self)

		etcd := view.InsertEtcd()
		etcd.Leader = true
		view.Commit(etcd)

		dbc := view.InsertContainer()
		dbc.Hostname = "host"
		dbc.IP = "10.0.0.2"
		dbc.Minion = "1.2.3.4"
		dbc.BlueprintID = "12"
		dbc.Image = "ubuntu"
		dbc.Command = []string{"1", "2", "3"}
		dbc.Env = map[string]blueprint.ContainerValue{
			"red":  blueprint.NewSecret("pill"),
			"blue": blueprint.NewString("pill"),
		}
		dbc.FilepathToContent = map[string]blueprint.ContainerValue{
			"foo": blueprint.NewString("bar"),
		}
		view.Commit(dbc)
		return nil
	})

	err = runContainerOnce(conn, store)
	assert.NoError(t, err)

	// Check that the container in the database was properly written into etcd.
	str, err := store.Get(containerPath)
	assert.NoError(t, err)

	expStr := `[
    {
        "IP": "10.0.0.2",
        "Minion": "1.2.3.4",
        "BlueprintID": "12",
        "Command": [
            "1",
            "2",
            "3"
        ],
        "Env": {
            "blue": "pill",
            "red": {
                "NameOfSecret": "pill"
            }
        },
        "FilepathToContent": {
            "foo": "bar"
        },
        "Hostname": "host",
        "Created": "0001-01-01T00:00:00Z",
        "Image": "ubuntu"
    }
]`
	assert.Equal(t, expStr, str)

	// Simulate reading from etcd as a non-leader Master minion. Note that the
	// etcd store was not reset (the string written by the master from the
	// above test is still present). Therefore, the changes to Env and
	// FilepathToContent in the local database should be overwritten by what is
	// in etcd.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.SelectFromEtcd(nil)[0]
		etcd.Leader = false
		view.Commit(etcd)

		dbc := view.SelectFromContainer(nil)[0]
		dbc.Env = map[string]blueprint.ContainerValue{
			"red":  blueprint.NewSecret("fish"),
			"blue": blueprint.NewString("fish"),
		}
		dbc.FilepathToContent = map[string]blueprint.ContainerValue{
			"bar": blueprint.NewString("baz"),
		}
		view.Commit(dbc)
		return nil
	})

	err = runContainerOnce(conn, store)
	assert.NoError(t, err)

	// Ensure that the minion properly synced the container from etcd.
	expDBC := db.Container{
		IP:          "10.0.0.2",
		BlueprintID: "12",
		Minion:      "1.2.3.4",
		Image:       "ubuntu",
		Command:     []string{"1", "2", "3"},
		Env: map[string]blueprint.ContainerValue{
			"red":  blueprint.NewSecret("pill"),
			"blue": blueprint.NewString("pill"),
		},
		FilepathToContent: map[string]blueprint.ContainerValue{
			"foo": blueprint.NewString("bar"),
		},
		Hostname: "host",
	}
	dbcs := conn.SelectFromContainer(nil)
	assert.Len(t, dbcs, 1)
	dbcs[0].ID = 0
	assert.Equal(t, expDBC, dbcs[0])

	// Run the same non-leader sync test again to make sure the result is
	// consistent.
	err = runContainerOnce(conn, store)
	assert.NoError(t, err)

	dbcs = conn.SelectFromContainer(nil)
	assert.Len(t, dbcs, 1)
	dbcs[0].ID = 0
	assert.Equal(t, expDBC, dbcs[0])

	// Check that syncing etcd from a Worker minion for which the container is
	// scheduled properly syncs the container.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		self := view.MinionSelf()
		self.Role = db.Worker
		self.PrivateIP = "1.2.3.4"
		view.Commit(self)
		return nil
	})

	err = runContainerOnce(conn, store)
	assert.NoError(t, err)

	dbcs = conn.SelectFromContainer(nil)
	assert.Len(t, dbcs, 1)
	dbcs[0].ID = 0
	assert.Equal(t, expDBC, dbcs[0])

	// Check that syncing etcd from a Worker minion for which the container is
	// _not_ scheduled does not sync the container. The minion should remove
	// the now irrelevant Container row.
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		self := view.MinionSelf()
		self.PrivateIP = "1.2.3.5"
		view.Commit(self)
		return nil
	})

	err = runContainerOnce(conn, store)
	assert.NoError(t, err)

	dbcs = conn.SelectFromContainer(nil)
	assert.Len(t, dbcs, 0)
}
