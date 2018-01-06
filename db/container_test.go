package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/blueprint"
)

func TestContainerString(t *testing.T) {
	c := Container{}
	got := c.String()
	exp := "Container-0{run }"
	assert.Equal(t, got, exp)

	fakeMap := make(map[string]blueprint.ContainerValue)
	fakeMap["test"] = blueprint.NewString("tester")
	fakeTime := time.Now()
	fakeTimeString := fakeTime.String()

	c = Container{
		ID:          1,
		IP:          "1.2.3.4",
		Minion:      "Test",
		BlueprintID: "1",
		PodName:     "PodName",
		Image:       "test/test",
		Status:      "testing",
		Hostname:    "hostname",
		Command:     []string{"run", "/bin/sh"},
		Env:         fakeMap,
		Created:     fakeTime,
	}

	exp = "Container-1{run test/test run /bin/sh, " +
		"PodName: PodName, Minion: Test, BlueprintID: 1, IP: 1.2.3.4, " +
		"Hostname: hostname, Env: map[test:tester], " +
		"Status: testing, Created: " + fakeTimeString + "}"

	assert.Equal(t, exp, c.String())
}

func TestContainerHelpers(t *testing.T) {
	conn := New()
	conn.Txn(AllTables...).Run(func(view Database) error {
		c := view.InsertContainer()
		c.IP = "foo"
		view.Commit(c)
		return nil
	})

	dbcs := conn.SelectFromContainer(func(c Container) bool {
		return true
	})
	assert.Len(t, dbcs, 1)
	assert.Equal(t, "foo", dbcs[0].IP)
}

func TestGetReferencedSecrets(t *testing.T) {
	t.Parallel()

	secret1 := "secret1"
	secret2 := "secret2"
	secret3 := "secret3"
	secret4 := "secret4"
	dbc := Container{
		Env: map[string]blueprint.ContainerValue{
			"key1": blueprint.NewString("ignoreme"),
			"key2": blueprint.NewSecret(secret1),
			"key3": blueprint.NewSecret(secret2),
		},
		FilepathToContent: map[string]blueprint.ContainerValue{
			"key1": blueprint.NewString("ignoreme"),
			"key3": blueprint.NewSecret(secret3),
			"key4": blueprint.NewSecret(secret4),
		},
	}
	referencedSecrets := dbc.GetReferencedSecrets()
	assert.Len(t, referencedSecrets, 4)
	assert.Contains(t, referencedSecrets, secret1)
	assert.Contains(t, referencedSecrets, secret2)
	assert.Contains(t, referencedSecrets, secret3)
	assert.Contains(t, referencedSecrets, secret4)
}
