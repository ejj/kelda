package util

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/quilt/quilt/api/client/mocks"
	"github.com/quilt/quilt/db"
)

func TestGetContainer(t *testing.T) {
	t.Parallel()

	a := db.Container{StitchID: "4567"}
	b := db.Container{StitchID: "432"}
	c := &mocks.Client{ContainerReturn: []db.Container{a, b}}

	res, err := GetContainer(c, "4567")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = GetContainer(c, "456")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = GetContainer(c, "45")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = GetContainer(c, "432")
	assert.Nil(t, err)
	assert.Equal(t, b, res)

	res, err = GetContainer(c, "43")
	assert.Nil(t, err)
	assert.Equal(t, b, res)

	_, err = GetContainer(c, "4")
	assert.EqualError(t, err, `ambiguous stitchIDs 4567 and 432`)

	_, err = GetContainer(c, "1")
	assert.EqualError(t, err, `no container with stitchID "1"`)
}
