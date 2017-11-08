package str

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSliceFilter(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SliceFilterOut([]string{"a", "b", "a"}, "a"), []string{"b"})
	assert.Equal(t, SliceFilterOut([]string{"a", "b"}, "c"), []string{"a", "b"})
}

func TestSliceContains(t *testing.T) {
	t.Parallel()

	assert.True(t, SliceContains([]string{"b", "a"}, "a"))
	assert.False(t, SliceContains([]string{"b", "a"}, "c"))
}

func TestSliceCmp(t *testing.T) {
	t.Parallel()

	x := []string{}
	y := []string{}

	assert.Zero(t, SliceCmp(x, y))
	assert.True(t, SliceEq(x, y))

	x = append(x, "1")
	assert.Equal(t, 1, SliceCmp(x, y))
	assert.Equal(t, -1, SliceCmp(y, x))
	assert.False(t, SliceEq(x, y))

	y = append(y, "1")
	assert.Zero(t, SliceCmp(x, y))
	assert.True(t, SliceEq(x, y))

	x = append(x, "a")
	y = append(y, "b")
	assert.Equal(t, -1, SliceCmp(x, y))
	assert.Equal(t, 1, SliceCmp(y, x))
	assert.False(t, SliceEq(x, y))
}

func TestMapEqual(t *testing.T) {
	t.Parallel()

	a := map[string]string{}
	b := map[string]string{}

	assert.True(t, MapEq(a, b))

	a["1"] = "1"
	assert.False(t, MapEq(a, b))

	b["1"] = a["1"]
	assert.True(t, MapEq(a, b))

	b["1"] = "2"
	assert.False(t, MapEq(a, b))
}

func TestMapString(t *testing.T) {
	t.Parallel()

	// Run the tests multiple times to test determinism.
	for i := 0; i < 10; i++ {
		assert.Equal(t, "[a=1 b=2]", MapAsString(
			map[string]string{"a": "1", "b": "2"}))

		// Nil and empty maps are the same.
		assert.Equal(t, "[]", MapAsString(nil))
		assert.Equal(t, "[]", MapAsString(map[string]string{}))
	}
}
