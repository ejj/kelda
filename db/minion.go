package db

// The Minion table is instantiated on the minions with one row.  That row contains the
// configuration that minion needs to operate, including its ID, Role, and IP address
type Minion struct {
	ID int `json:"-"`

	Self           bool   `json:"-"`
	Blueprint      string `json:"-" rowStringer:"omit"`
	AuthorizedKeys string `json:"-" rowStringer:"omit"`

	// Below fields are included in the JSON encoding.
	Role        Role
	PrivateIP   string
	Provider    string
	Size        string
	Region      string
	FloatingIP  string
	HostSubnets []string
}

// InsertMinion creates a new Minion and inserts it into 'db'.
func (db Database) InsertMinion() Minion {
	result := Minion{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromMinion gets all minions in the database that satisfy the 'check'.
func (db Database) SelectFromMinion(check func(Minion) bool) []Minion {
	var result []Minion
	for _, row := range db.selectRows(MinionTable) {
		if check == nil || check(row.(Minion)) {
			result = append(result, row.(Minion))
		}
	}
	return result
}

// MinionSelf returns the Minion Row corresponding to the currently running minion.
// If there is no Minion Row, it panics
func (db Database) MinionSelf() Minion {
	minions := db.SelectFromMinion(func(m Minion) bool {
		return m.Self
	})

	if len(minions) > 1 {
		panic("multiple minions labeled Self")
	}

	if len(minions) == 0 {
		panic("no minion labeled Self")
	}

	return minions[0]
}

// MinionSelf returns the Minion Row corresponding to the currently running minion.
// If there is no Minion Row, it panics
func (conn Conn) MinionSelf() Minion {
	var m Minion

	conn.Txn(MinionTable).Run(func(view Database) error {
		m = view.MinionSelf()
		return nil
	})

	return m
}

// SelectFromMinion gets all minions in the database that satisfy the 'check'.
func (conn Conn) SelectFromMinion(check func(Minion) bool) []Minion {
	var minions []Minion
	conn.Txn(MinionTable).Run(func(view Database) error {
		minions = view.SelectFromMinion(check)
		return nil
	})
	return minions
}

func (m Minion) getID() int {
	return m.ID
}

func (m Minion) String() string {
	return defaultString(m)
}

func (m Minion) less(r row) bool {
	return m.ID < r.(Minion).ID
}

// MinionSlice is an alias for []Minion to allow for joins
type MinionSlice []Minion

// Get returns the value contained at the given index
func (m MinionSlice) Get(ii int) interface{} {
	return m[ii]
}

// Len returns the number of items in the slice
func (m MinionSlice) Len() int {
	return len(m)
}
