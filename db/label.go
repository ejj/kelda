package db

// A Label row is created for each label specified by the policy.
type Label struct {
	ID int

	Label        string
	IP           string
	ContainerIPs []string
}

// LabelSlice is an alias for []Label to allow for joins
type LabelSlice []Label

// InsertLabel creates a new label row and inserts it into the database.
func (db Database) InsertLabel() Label {
	result := Label{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromLabel gets all labels in the database that satisfy 'check'.
func (db Database) SelectFromLabel(check func(Label) bool) []Label {
	labelTable := db.accessTable(LabelTable)
	var result []Label
	for _, row := range labelTable.rows {
		if check == nil || check(row.(Label)) {
			result = append(result, row.(Label))
		}
	}

	return result
}

// SelectFromLabel gets all labels in the database connection that satisfy 'check'.
func (conn Conn) SelectFromLabel(check func(Label) bool) []Label {
	var result []Label
	conn.Txn(LabelTable).Run(func(view Database) error {
		result = view.SelectFromLabel(check)
		return nil
	})
	return result
}

func (r Label) getID() int {
	return r.ID
}

func (r Label) String() string {
	return defaultString(r)
}

func (r Label) less(row row) bool {
	r2 := row.(Label)

	switch {
	case r.Label != r2.Label:
		return r.Label < r2.Label
	default:
		return r.ID < r2.ID
	}
}

// Get returns the value contained at the given index
func (ls LabelSlice) Get(i int) interface{} {
	return ls[i]
}

// Len returns the number of items in the slice
func (ls LabelSlice) Len() int {
	return len(ls)
}

// Less implements less than for sort.Interface.
func (ls LabelSlice) Less(i, j int) bool {
	return ls[i].less(ls[j])
}

// Swap implements swapping for sort.Interface.
func (ls LabelSlice) Swap(i, j int) {
	ls[i], ls[j] = ls[j], ls[i]
}
