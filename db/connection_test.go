package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnection(t *testing.T) {
	t.Parallel()

	conn := New()

	var id int
	conn.Txn(ConnectionTable).Run(func(view Database) error {
		connection := view.InsertConnection()
		id = connection.ID
		connection.From = []string{"foo"}
		view.Commit(connection)
		return nil
	})

	connections := ConnectionSlice(conn.SelectFromConnection(
		func(i Connection) bool { return true }))
	assert.Equal(t, 1, connections.Len())

	connection := connections[0]
	assert.Equal(t, []string{"foo"}, connection.From)
	assert.Equal(t, id, connection.getID())

	connection.MaxPort = 3
	assert.Equal(t, "Connection-1{[foo]->[]:0-3}", connection.String())
	connection.MaxPort = 0

	assert.Equal(t, connection, connections.Get(0))

	assert.True(t, connection.less(Connection{From: []string{"z"}}))
	assert.True(t, connection.less(Connection{From: []string{"foo"},
		To: []string{"a"}}))
	assert.True(t, connection.less(Connection{From: []string{"foo"}, MaxPort: 1}))
	assert.True(t, connection.less(Connection{From: []string{"foo"}, MinPort: 100}))
	assert.True(t, connection.less(Connection{From: []string{"foo"}, ID: id + 1}))

	assert.True(t, connection.less(Connection{From: []string{"foo", "bar"}}))
	assert.True(t, connection.less(Connection{From: []string{"foo"},
		To: []string{"baz", "qux"}}))

}
