package db

import (
	"fmt"

	"github.com/kelda/kelda/util/str"
)

// A Connection allows two hostnames to speak to each other on the port
// range [MinPort, MaxPort] inclusive.
type Connection struct {
	ID int `json:"-"`

	From    []string
	To      []string
	MinPort int
	MaxPort int
}

// InsertConnection creates a new connection row and inserts it into the database.
func (db Database) InsertConnection() Connection {
	result := Connection{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromConnection gets all connections in the database that satisfy 'check'.
func (db Database) SelectFromConnection(check func(Connection) bool) []Connection {
	var result []Connection
	for _, row := range db.selectRows(ConnectionTable) {
		if check == nil || check(row.(Connection)) {
			result = append(result, row.(Connection))
		}
	}

	return result
}

func (c Connection) getID() int {
	return c.ID
}

// SelectFromConnection gets all connections in the database connection that satisfy
// the 'check'.
func (conn Conn) SelectFromConnection(check func(Connection) bool) []Connection {
	var connections []Connection
	conn.Txn(ConnectionTable).Run(func(view Database) error {
		connections = view.SelectFromConnection(check)
		return nil
	})
	return connections
}

func (c Connection) String() string {
	port := fmt.Sprintf("%d", c.MinPort)
	if c.MaxPort != c.MinPort {
		port += fmt.Sprintf("-%d", c.MaxPort)
	}

	return fmt.Sprintf("Connection-%d{%s->%s:%s}", c.ID, c.From, c.To, port)
}

func (c Connection) less(r row) bool {
	o := r.(Connection)

	if cmp := str.SliceCmp(c.From, o.From); cmp != 0 {
		return cmp < 0
	} else if cmp := str.SliceCmp(c.To, o.To); cmp != 0 {
		return cmp < 0
	}

	switch {
	case c.MaxPort != o.MaxPort:
		return c.MaxPort < o.MaxPort
	case c.MinPort != o.MinPort:
		return c.MinPort < o.MinPort
	default:
		return c.ID < o.ID
	}
}

// ConnectionSlice is an alias for []Connection to allow for joins
type ConnectionSlice []Connection

// Get returns the value contained at the given index
func (cs ConnectionSlice) Get(i int) interface{} {
	return cs[i]
}

// Len returns the number of items in the slice.
func (cs ConnectionSlice) Len() int {
	return len(cs)
}

// Less implements less than for sort.Interface.
func (cs ConnectionSlice) Less(i, j int) bool {
	return cs[i].less(cs[j])
}

// Swap implements swapping for sort.Interface.
func (cs ConnectionSlice) Swap(i, j int) {
	cs[i], cs[j] = cs[j], cs[i]
}
