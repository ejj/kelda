package db

// Placement represents a declaration about how containers should be placed.  These
// directives can be made either relative to labels of other containers, or Machines
// those containers run on.
type Placement struct {
	ID int

	TargetLabel string

	Exclusive bool

	// Label Constraint
	OtherLabel string

	// Machine Constraints
	Provider string
	Size     string
	Region   string
}

// PlacementSlice is an alias for []Placement to allow for joins
type PlacementSlice []Placement

// InsertPlacement creates a new placement row and inserts it into the database.
func (db Database) InsertPlacement() Placement {
	result := Placement{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromPlacement gets all placements in the database that satisfy 'check'.
func (db Database) SelectFromPlacement(check func(Placement) bool) []Placement {
	var result []Placement
	for _, row := range db.tables[PlacementTable].rows {
		if check == nil || check(row.(Placement)) {
			result = append(result, row.(Placement))
		}
	}

	return result
}

// SelectFromPlacement gets all placements in the database that satisfy the 'check'.
func (conn Conn) SelectFromPlacement(check func(Placement) bool) []Placement {
	var placements []Placement
	conn.Transact(func(view Database) error {
		placements = view.SelectFromPlacement(check)
		return nil
	})
	return placements
}

func (p Placement) String() string {
	return defaultString(p)
}

func (p Placement) less(r row) bool {
	return p.ID < r.(Placement).ID
}

func (p Placement) getID() int {
	return p.ID
}

// Get returns the value contained at the given index
func (ps PlacementSlice) Get(ii int) interface{} {
	return ps[ii]
}

// Len returns the numebr of items in the slice
func (ps PlacementSlice) Len() int {
	return len(ps)
}
