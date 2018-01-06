package util

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/db"
)

func TestFuzzyLookup(t *testing.T) {
	client := new(mocks.Client)
	client.On("QueryMachines").Return(nil, assert.AnError)
	client.On("QueryContainers").Return(nil, assert.AnError)

	_, err := FuzzyLookup(client, "foo")
	assert.Error(t, err)

	container := db.Container{BlueprintID: "13", Minion: "1.2.3.4"}
	client = new(mocks.Client)
	client.On("QueryContainers").Return([]db.Container{container}, nil)
	machine := db.Machine{CloudID: "12", PrivateIP: "1.2.3.4", PublicIP: "5.6.7.8"}
	client.On("QueryMachines").Return([]db.Machine{machine}, nil)

	_, err = FuzzyLookup(client, "1")
	assert.EqualError(t, err, "ambiguous IDs: machine \"12\", container \"13\"")

	i, err := FuzzyLookup(client, "12")
	assert.NoError(t, err)
	assert.Equal(t, machine, i)

	i, err = FuzzyLookup(client, "13")
	assert.NoError(t, err)
	assert.Equal(t, container, i)
}

func TestFindContainer(t *testing.T) {
	t.Parallel()

	a := db.Container{BlueprintID: "4567", Hostname: "a"}
	b := db.Container{BlueprintID: "432", Hostname: "b"}

	res, err := findContainer([]db.Container{a, b}, "4567")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = findContainer([]db.Container{a, b}, "456")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = findContainer([]db.Container{a, b}, "45")
	assert.Nil(t, err)
	assert.Equal(t, a, res)

	res, err = findContainer([]db.Container{a, b}, "432")
	assert.Nil(t, err)
	assert.Equal(t, b, res)

	res, err = findContainer([]db.Container{a, b}, "43")
	assert.Nil(t, err)
	assert.Equal(t, b, res)

	res, err = findContainer([]db.Container{a, b}, "a")
	assert.NoError(t, err)
	assert.Equal(t, a, res)

	_, err = findContainer([]db.Container{a, b}, "4")
	assert.EqualError(t, err, `ambiguous choices 4567 and 432`)

	_, err = findContainer([]db.Container{a, b}, "1")
	assert.EqualError(t, err, `no container "1"`)
}
