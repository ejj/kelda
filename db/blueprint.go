package db

import (
	"errors"
	"log"

	"github.com/kelda/kelda/blueprint"
)

// A Blueprint that Kelda is attempting to implement.
type Blueprint struct {
	ID int

	blueprint.Blueprint `rowStringer:"omit"`
}

// InsertBlueprint creates a new Blueprint and interts it into 'db'.
func (db Database) InsertBlueprint() Blueprint {
	result := Blueprint{ID: db.nextID()}
	db.insert(result)
	return result
}

// SelectFromBlueprint gets all blueprints in the database that satisfy 'check'.
func (db Database) SelectFromBlueprint(check func(Blueprint) bool) []Blueprint {
	var result []Blueprint
	for _, row := range db.selectRows(BlueprintTable) {
		if check == nil || check(row.(Blueprint)) {
			result = append(result, row.(Blueprint))
		}
	}
	return result
}

// SelectFromBlueprint gets all blueprints in the database that satisfy 'check'.
func (conn Conn) SelectFromBlueprint(check func(Blueprint) bool) []Blueprint {
	var blueprints []Blueprint
	conn.Txn(BlueprintTable).Run(func(view Database) error {
		blueprints = view.SelectFromBlueprint(check)
		return nil
	})
	return blueprints
}

// GetBlueprint gets the blueprint from the database. There should only ever be a single
// blueprint.
func (db Database) GetBlueprint() (Blueprint, error) {
	blueprints := db.SelectFromBlueprint(nil)
	numBlueprints := len(blueprints)
	if numBlueprints == 1 {
		return blueprints[0], nil
	} else if numBlueprints > 1 {
		log.Panicf("Found %d blueprints, there should be 1", numBlueprints)
	}
	return Blueprint{}, errors.New("no blueprints found")
}

// GetBlueprintNamespace returns the namespace of the single blueprint object in the
// blueprint table.  Otherwise it returns an error.
func (db Database) GetBlueprintNamespace() (string, error) {
	clst, err := db.GetBlueprint()
	if err != nil {
		return "", err
	}
	return clst.Namespace, nil
}

// GetBlueprintNamespace returns the namespace of the single blueprint object in the
// blueprint table.  Otherwise it returns an error.
func (conn Conn) GetBlueprintNamespace() (namespace string, err error) {
	conn.Txn(BlueprintTable).Run(func(db Database) error {
		namespace, err = db.GetBlueprintNamespace()
		return nil
	})
	return
}

func (b Blueprint) getID() int {
	return b.ID
}

func (b Blueprint) tt() TableType {
	return BlueprintTable
}

func (b Blueprint) String() string {
	return defaultString(b)
}

func (b Blueprint) less(r row) bool {
	return b.ID < r.(Blueprint).ID
}
