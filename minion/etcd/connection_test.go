package etcd

import (
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/stretchr/testify/assert"
)

func TestRunConnectionOnce(t *testing.T) {
	t.Parallel()

	store := newTestMock()
	conn := db.New()

	err := runConnectionOnce(conn, store)
	assert.Error(t, err)

	err = store.Set(connectionPath, "", 0)
	assert.NoError(t, err)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.InsertEtcd()
		etcd.Leader = true
		view.Commit(etcd)

		conn := view.InsertConnection()
		conn.From = "a"
		conn.To = "b"
		conn.MinPort = 80
		conn.MaxPort = 8080
		view.Commit(conn)
		return nil
	})

	err = runConnectionOnce(conn, store)
	assert.NoError(t, err)

	str, err := store.Get(connectionPath)
	assert.NoError(t, err)

	expStr := `[
    {
        "From": "a",
        "To": "b",
        "MinPort": 80,
        "MaxPort": 8080
    }
]`
	assert.Equal(t, expStr, str)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.SelectFromEtcd(nil)[0]
		etcd.Leader = false
		view.Commit(etcd)

		conn := view.SelectFromConnection(nil)[0]
		conn.From = "1"
		conn.To = "2"
		conn.MinPort = 3
		conn.MaxPort = 4
		view.Commit(conn)
		return nil
	})

	err = runConnectionOnce(conn, store)
	assert.NoError(t, err)

	conns := conn.SelectFromConnection(nil)
	assert.Len(t, conns, 1)
	conns[0].ID = 0
	assert.Equal(t, db.Connection{From: "a", To: "b", MinPort: 80, MaxPort: 8080},
		conns[0])
}
