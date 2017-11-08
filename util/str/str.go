package str

import (
	"fmt"
	"sort"
)

// SliceFilterOut returns a new slice that omits all instances of `item`.
func SliceFilterOut(slice []string, item string) []string {
	var result []string
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

// SliceContains returns true if `item` is in `slice`.
func SliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// SliceEq returns true of the string slices 'x' and 'y' are identical.
func SliceEq(x, y []string) bool {
	return SliceCmp(x, y) == 0
}

// SliceCmp applies an arbitrary ordering to slices of strings.  Returns -1 if x < y, 1
// if x > y, 0 otherwise.
func SliceCmp(x, y []string) int {
	if len(x) < len(y) {
		return -1
	} else if len(x) > len(y) {
		return 1
	}

	for i, v := range x {
		if v < y[i] {
			return -1
		} else if v > y[i] {
			return 1
		}
	}

	return 0
}

// MapEq returns true if the string->string maps 'x' and 'y' are equal.
func MapEq(x, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if yVal, ok := y[k]; !ok || v != yVal {
			return false
		}
	}
	return true
}

// MapAsString creates a deterministic string representing the given map.
func MapAsString(m map[string]string) string {
	var strs []string
	for k, v := range m {
		strs = append(strs, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Sort(sort.StringSlice(strs))
	return fmt.Sprintf("%v", strs)
}
