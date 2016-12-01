package ovsdb

import (
	"reflect"

	uuid "github.com/satori/go.uuid"
	ovs "github.com/socketplane/libovsdb"
)

// NewFakeOvsdbClient returns an ovsdb client with mocked ovsdb.
func NewFakeOvsdbClient() Client {
	// Clears uuidMap for new test.
	uuidMap = map[string]string{}

	return Client{fakeOvsdbClient{
		databases: map[string]fakeDb{},
	}}
}

// Default OFPort used during creation of interface.
const defaultOFPort float64 = 123

type fakeOvsdbClient struct {
	databases map[string]fakeDb
}

type fakeDb struct {
	tables map[string]fakeTable
}

type fakeTable struct {
	rows map[string]fakeRow
}

type fakeRow map[string]interface{}

// Mapping of UUIDName:UUID.
var uuidMap map[string]string

type fakeCondition struct {
	column, function, value string
}

func (cond fakeCondition) matches(row fakeRow) bool {
	toMatch := ""

	switch v := row[cond.column].(type) {
	case string:
		toMatch = v
	case []interface{}:
		if cond.column == "_uuid" {
			toMatch = v[1].(string)
		} else {
			panic("condition on this column is not yet supported: " +
				cond.column)
		}
	default:
		panic("condition type is not yet supported:" +
			reflect.TypeOf(row[cond.column]).String())
	}

	switch cond.function {
	case "==":
		return toMatch == cond.value
	case "!=":
		return toMatch != cond.value
	default:
		panic("condition func is not yet supported: " + cond.function)
	}
}

type fakeMutation struct {
	column  string
	mutator string
	value   interface{}
}

// Applies mutation on a row, and returns number of mutations applied.
func (mutation fakeMutation) apply(row *fakeRow) int {
	switch mutationVal := mutation.value.(type) {
	case *ovs.OvsSet:
		return mutation.applySet(row, mutationVal)
	case *ovs.OvsMap:
		return mutation.applyMap(row, mutationVal)
	default:
		panic("mutation type is not yet supported:" +
			reflect.TypeOf(mutationVal).String())
	}
}

func (mutation fakeMutation) applySet(row *fakeRow, set *ovs.OvsSet) int {
	mutationCount := 0
	column := (*row)[mutation.column].([]interface{})
	data := column[1].([]interface{})

	uuidGeneric := set.GoSet[0].(ovs.UUID).GoUUID
	uuid, ok := uuidMap[uuidGeneric]
	if !ok {
		uuid = uuidGeneric
	}
	switch mutation.mutator {
	case "insert":
		data = append(data, []interface{}{"uuid", uuid})
		mutationCount++
	case "delete":
		for i, e := range data {
			if e.([]interface{})[1] == uuid {
				data = append(data[:i], data[i+1:]...)
				break
			}
		}
		mutationCount++
	default:
		panic("mutator is not yet supported:" + mutation.mutator)
	}

	column[1] = data
	(*row)[mutation.column] = column
	return mutationCount
}

func (mutation fakeMutation) applyMap(row *fakeRow, mp *ovs.OvsMap) int {
	mutationCount := 0
	column := (*row)[mutation.column].([]interface{})

	switch data := column[1].(type) {
	case map[string]interface{}:
		for k, v := range mp.GoMap {
			switch mutation.mutator {
			case "insert":
				data[k.(string)] = v
				mutationCount++
			default:
				panic("mutator is not yet supported:" +
					mutation.mutator)
			}
		}
		column[1] = data
	case []interface{}:
		for k, v := range mp.GoMap {
			switch mutation.mutator {
			case "insert":
				data = append(data, []interface{}{k, v})
				mutationCount++
			default:
				panic("mutator is not yet supported:" +
					mutation.mutator)
			}
		}
		column[1] = data
	default:
		panic("not yet supported.")
	}

	(*row)[mutation.column] = column
	return mutationCount
}

func (client fakeOvsdbClient) disconnect() {
	// Nothing.
}

func (client fakeOvsdbClient) transact(database string, operation ...ovs.Operation) (
	[]ovs.OperationResult, error) {
	opResults := []ovs.OperationResult{}
	for _, op := range operation {
		var result ovs.OperationResult
		var err error
		switch op.Op {
		case "insert":
			result, err = client.insertOp(database, op)
		case "select":
			result, err = client.selectOp(database, op)
		case "update":
			result, err = client.updateOp(database, op)
		case "mutate":
			result, err = client.mutateOp(database, op)
		case "delete":
			result, err = client.deleteOp(database, op)
		case "wait":
			panic("operation is not yet supported: wait")
		case "commit":
			panic("operation is not yet supported: commit")
		case "abort":
			panic("operation is not yet supported: abort")
		case "comment":
			panic("operation is not yet supported: comment")
		case "assert":
			panic("operation is not yet supported: assert")
		default:
			panic("operation is not supported in RFC 7047:" + op.Op)
		}

		if err != nil {
			return nil, err
		}
		opResults = append(opResults, result)
	}

	return opResults, nil
}

// Only support insert single row for now.
// RFC 7047: insert operation returns field UUID only.
func (client fakeOvsdbClient) insertOp(database string, op ovs.Operation) (
	ovs.OperationResult, error) {
	db, ok := client.databases[database]
	if !ok {
		db = fakeDb{
			tables: map[string]fakeTable{},
		}
	}

	table, ok := db.tables[op.Table]
	if !ok {
		table = fakeTable{
			rows: map[string]fakeRow{},
		}
	}

	uuid := uuid.NewV4().String()
	row := fakeRow{}
	row["_uuid"] = []interface{}{"uuid", uuid}

	for newKey, newVal := range op.Row {
		switch v := newVal.(type) {
		case string, bool:
			row[newKey] = newVal
		case *ovs.OvsSet:
			var goSet []interface{}
			switch v.GoSet[0].(type) {
			case ovs.UUID:
				for _, elem := range v.GoSet {
					var uuidToAdd string
					uuidGeneric := elem.(ovs.UUID).GoUUID
					if actualUUID, ok := uuidMap[uuidGeneric]; ok {
						uuidToAdd = actualUUID
					} else {
						uuidToAdd = uuidGeneric
					}
					goSet = append(goSet,
						[]interface{}{"uuid", uuidToAdd})
				}
			default:
				goSet = v.GoSet
			}
			row[newKey] = append([]interface{}{"set"}, goSet)
		case int:
			row[newKey] = float64(v)
		default:
			panic("insert value type is not yet supported: " +
				reflect.TypeOf(v).String())
		}
	}

	// Mimic ovsdb's behavior of adding default fields for each table.
	switch op.Table {
	case "Logical_Switch":
		row["ports"] = []interface{}{"set", []interface{}{}}
		row["acls"] = []interface{}{"set", []interface{}{}}
	case "Bridge":
		row["ports"] = []interface{}{"set", []interface{}{}}
		row["other_config"] = []interface{}{"map", map[string]interface{}{}}
	case "Interface":
		row["type"] = ""
		row["options"] = []interface{}{"map", []interface{}{}}
		row["external_ids"] = []interface{}{"map", []interface{}{}}
		// An ofport is assigned to interface during creation.
		row["ofport"] = defaultOFPort
	}

	table.rows[uuid] = row

	// Update UUIDName if existed.
	if op.UUIDName != "" {
		uuidMap[op.UUIDName] = uuid
	}

	// Update table and db.
	db.tables[op.Table] = table
	client.databases[database] = db

	return ovs.OperationResult{UUID: ovs.UUID{GoUUID: uuid}}, nil
}

// RFC 7047: select operation returns field rows only.
func (client fakeOvsdbClient) selectOp(database string, op ovs.Operation) (
	ovs.OperationResult, error) {
	db, ok := client.databases[database]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	table, ok := db.tables[op.Table]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	result := ovs.OperationResult{}
Outer:
	for _, row := range table.rows {
		conditions := parseOperationCondition(op)
		for _, cond := range conditions {
			if !cond.matches(row) {
				continue Outer
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

// RFC 7047: update operation returns field count only.
func (client fakeOvsdbClient) updateOp(database string, op ovs.Operation) (
	ovs.OperationResult, error) {
	db, ok := client.databases[database]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	table, ok := db.tables[op.Table]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	updateCount := 0
Outer:
	for _, row := range table.rows {
		conditions := parseOperationCondition(op)
		for _, cond := range conditions {
			if !cond.matches(row) {
				continue Outer
			}
		}
		for k, v := range op.Row {
			row[k] = v
			updateCount++
		}
	}
	return ovs.OperationResult{Count: updateCount}, nil
}

// RFC 7047: select operation returns field count only.
func (client fakeOvsdbClient) mutateOp(database string, op ovs.Operation) (
	ovs.OperationResult, error) {
	db, ok := client.databases[database]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	table, ok := db.tables[op.Table]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	mutations := parseOperationMutations(op)
	mutationCount := 0

Outer:
	for rowUUID, row := range table.rows {
		conditions := parseOperationCondition(op)
		for _, cond := range conditions {
			if !cond.matches(row) {
				continue Outer
			}
		}
		for _, mutation := range mutations {
			mutation.apply(&row)
			mutationCount++
		}
		table.rows[rowUUID] = row
	}

	// Update table and db.
	db.tables[op.Table] = table
	client.databases[database] = db

	return ovs.OperationResult{Count: mutationCount}, nil
}

// RFC 7047: delete operation returns field count only.
func (client fakeOvsdbClient) deleteOp(database string, op ovs.Operation) (
	ovs.OperationResult, error) {
	db, ok := client.databases[database]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	table, ok := db.tables[op.Table]
	if !ok {
		return ovs.OperationResult{}, nil
	}

	mutationCount := 0

Outer:
	for uuid, row := range table.rows {
		conditions := parseOperationCondition(op)
		for _, cond := range conditions {
			if !cond.matches(row) {
				continue Outer
			}
		}
		delete(table.rows, uuid)
		mutationCount++
	}

	// Update table and db.
	db.tables[op.Table] = table
	client.databases[database] = db

	return ovs.OperationResult{Count: mutationCount}, nil
}

func parseOperationCondition(op ovs.Operation) []fakeCondition {
	conditions := []fakeCondition{}
	for _, condGeneric := range op.Where {
		condition := condGeneric.([]interface{})
		column := condition[0].(string)
		function := condition[1].(string)
		value := ""

		switch v := condition[2].(type) {
		case ovs.UUID:
			value = v.GoUUID
		case string:
			value = v
		default:
			panic("parse condition is not yet supported:" +
				reflect.TypeOf(condition[2]).String())
		}

		conditions = append(conditions, fakeCondition{
			column:   column,
			function: function,
			value:    value,
		})
	}
	return conditions
}

func parseOperationMutations(op ovs.Operation) []fakeMutation {
	mutations := []fakeMutation{}
	for _, mutGeneric := range op.Mutations {
		mutation := mutGeneric.([]interface{})
		mutations = append(mutations, fakeMutation{
			column:  mutation[0].(string),
			mutator: mutation[1].(string),
			value:   mutation[2],
		})
	}
	return mutations
}
