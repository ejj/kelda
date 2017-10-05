// Package join implements a generic interface for matching elements from two slices
// similar in spirit to a database Join.
package join

import (
	"reflect"

	"sort"

	"github.com/kelda/kelda/counter"
)

var c = counter.New("Join")

// A Pair represents an element from the left slice and an element from the right slice,
// that have been matched by a join.
type Pair struct {
	L, R interface{}
}

// Join attempts to match each element in `lSlice` with an element in `rSlice` in
// accordance with a score function.  If such a match is found, it is returned as an
// element of `pairs`, while leftover elements from `lSlice` and `rSlice` that couldn`t
// be matched, are returned as elements of `lonelyLefts` and `lonelyRights`
// respectively. Both `lSlice` and `rSlice` must be slice or array types, but they do
// not necessarily have to have the same type.
//
// Matches are made in accordance with the provided `score` function.  It takes a single
// element from `lSlice`, and a single element from `rSlice`, and computes a score
// representing the match priority.  The algorithm strictly prioritizes lower scoring
// matches first, but negative scores are never matched. The algorithm does not minimize
// the total score of all matches.
func Join(lSlice, rSlice interface{}, score func(left, right interface{}) int) (
	pairs []Pair, lonelyLefts, lonelyRights []interface{}) {
	c.Inc("Join")

	type scoredPair struct {
		left  int
		right int
		score int
	}

	left := reflect.ValueOf(lSlice)
	right := reflect.ValueOf(rSlice)
	pairedLefts := map[int]struct{}{}
	pairedRights := map[int]struct{}{}

	scoredPairs := []scoredPair{}
	pairs = []Pair{}

	// Generate initial list of pairs.
OuterPairing:
	for i := 0; i < left.Len(); i++ {
		for j := 0; j < right.Len(); j++ {
			if _, ok := pairedRights[j]; ok {
				continue
			}
			lVal := left.Index(i).Interface()
			rVal := right.Index(j).Interface()
			score := score(lVal, rVal)
			if score == 0 {
				// Pair immediately.
				pairs = append(pairs, Pair{lVal, rVal})
				pairedLefts[i] = struct{}{}
				pairedRights[j] = struct{}{}

				continue OuterPairing
			} else if score > 0 {
				scoredPairs = append(scoredPairs,
					scoredPair{i, j, score})
			}
		}
	}

	// Sort and collect 'best' pairs.
	sort.SliceStable(scoredPairs, func(i, j int) bool {
		return scoredPairs[i].score < scoredPairs[j].score
	})
	for _, scoredPair := range scoredPairs {
		if len(pairedLefts) == left.Len() || len(pairedRights) == right.Len() {
			break
		}
		if _, ok := pairedLefts[scoredPair.left]; ok {
			continue
		}
		if _, ok := pairedRights[scoredPair.right]; ok {
			continue
		}

		lVal := left.Index(scoredPair.left).Interface()
		rVal := right.Index(scoredPair.right).Interface()
		pairs = append(pairs, Pair{lVal, rVal})
		pairedLefts[scoredPair.left] = struct{}{}
		pairedRights[scoredPair.right] = struct{}{}
	}

	// Collect unpaired elements. Iterating over the original struct ensures
	// that lonelyLefts/lonelyRights are returned in a consistent order.
	lonelyLefts = make([]interface{}, 0, left.Len()-len(pairedLefts))
	lonelyRights = make([]interface{}, 0, right.Len()-len(pairedRights))
	for i := 0; i < left.Len(); i++ {
		if _, ok := pairedLefts[i]; !ok {
			lonelyLefts = append(lonelyLefts, left.Index(i).Interface())
		}
	}
	for i := 0; i < right.Len(); i++ {
		if _, ok := pairedRights[i]; !ok {
			lonelyRights = append(lonelyRights, right.Index(i).Interface())
		}
	}

	return pairs, lonelyLefts, lonelyRights
}

// List simply requires implementing types to allow access to their contained values by
// integer index.
type List interface {
	Len() int
	Get(int) interface{}
}

// HashJoin attempts to match each element in `lSlice` with an element in `rSlice` by
// performing a hash join. If such a match is found for a given element of `lSlice`,
// it is returned as an element of `pairs`, while leftover elements from `lSlice` and
// `rSlice` that couldn`t be matched are returned as elements of `lonelyLefts` and
// `lonelyRights` respectively. The join keys for `lSlice` and `rSlice` are defined by
// the passed in `lKey` and `rKey` functions, respectively.
//
// If `lKey` or `rKey` are nil, the elements of the respective slices are used directly
// as keys instead.
func HashJoin(lSlice, rSlice List, lKey, rKey func(interface{}) interface{}) (
	pairs []Pair, lonelyLefts, lonelyRights []interface{}) {
	c.Inc("HashJoin")

	var identity = func(val interface{}) interface{} {
		return val
	}

	if lKey == nil {
		lKey = identity
	}
	if rKey == nil {
		rKey = identity
	}

	// lonely lefts are tracked implicitly by remaining elements in joinTable
	joinTable := make(map[interface{}]*interface{})

	for ii := 0; ii < lSlice.Len(); ii++ {
		lElem := lSlice.Get(ii)
		joinTable[lKey(lElem)] = &lElem
	}

	// Query the join table and match pairs using rSlice.
	// As matches are found, remove from lonely lefts.
	// As matches are not found, add to lonely rights.
	for ii := 0; ii < rSlice.Len(); ii++ {
		rElem := rSlice.Get(ii)
		rElemKey := rKey(rElem)
		if entry, ok := joinTable[rElemKey]; ok {
			pairs = append(pairs, Pair{*entry, rElem})
			delete(joinTable, rElemKey) // ok since rElemKey == lElemKey here
		} else {
			lonelyRights = append(lonelyRights, rElem)
		}
	}

	// transform the lonely sets back into slices (note: random order!)
	for _, ll := range joinTable {
		lonelyLefts = append(lonelyLefts, *ll)
	}

	return pairs, lonelyLefts, lonelyRights
}

// StringSlice is an alias for []string to allow for joins
type StringSlice []string

// Get returns the value contained at the given index
func (ss StringSlice) Get(ii int) interface{} {
	return ss[ii]
}

// Len returns the number of items in the slice
func (ss StringSlice) Len() int {
	return len(ss)
}
