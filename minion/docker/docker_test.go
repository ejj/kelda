package docker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPull(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.PullError = true
	err := dk.Pull("foo")
	assert.NotNil(t, err)

	_, ok := dk.imageCache["foo"]
	assert.False(t, ok)
	md.PullError = false

	err = dk.Pull("foo")
	assert.Nil(t, err)

	exp := map[string]struct{}{
		"foo": {},
	}
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))

	err = dk.Pull("foo")
	assert.Nil(t, err)
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))
}

func checkCache(prePull func()) (bool, error) {
	testImage := "foo"
	md, dk := NewMock()

	if err := dk.Pull(testImage); err != nil {
		return false, err
	}

	delete(md.Pulled, testImage)

	prePull()
	if err := dk.Pull(testImage); err != nil {
		return false, err
	}

	_, pulled := md.Pulled[testImage]
	return !pulled, nil
}

func TestPullImageCached(t *testing.T) {
	cached, err := checkCache(func() {})
	assert.Nil(t, err)
	assert.True(t, cached)
}

func TestPullImageNotCached(t *testing.T) {
	pullCacheTimeout = 300 * time.Millisecond

	cached, err := checkCache(func() {
		time.Sleep(500 * time.Millisecond)
	})
	assert.Nil(t, err)
	assert.False(t, cached)
}

func TestCreateGet(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.PullError = true
	_, err := dk.create("name", "image", nil, nil, nil, nil)
	assert.NotNil(t, err)
	md.PullError = false

	md.CreateError = true
	_, err = dk.create("name", "image", nil, nil, nil, nil)
	assert.NotNil(t, err)
	md.CreateError = false

	_, err = dk.Get("awef")
	assert.NotNil(t, err)

	args := []string{"arg1"}
	env := map[string]struct{}{
		"envA=B": {},
	}
	labels := map[string]string{"label": "foo"}
	id, err := dk.create("name", "image", args, labels, env, nil)
	assert.Nil(t, err)

	container, err := dk.Get(id)
	assert.Nil(t, err)

	expContainer := Container{
		Name:   "name",
		ID:     id,
		Image:  "image",
		Args:   args,
		Env:    map[string]string{"envA": "B"},
		Labels: labels,
	}
	assert.Equal(t, expContainer, container)
}

func TestRun(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.CreateError = true
	_, err := dk.Run(RunOptions{})
	assert.NotNil(t, err)
	md.CreateError = false

	md.StartError = true
	_, err = dk.Run(RunOptions{})
	assert.NotNil(t, err)
	md.StartError = false

	md.ListError = true
	_, err = dk.List(nil)
	assert.NotNil(t, err)
	md.ListError = false

	md.ListError = true
	_, err = dk.IsRunning("foo")
	assert.NotNil(t, err)
	md.ListError = false

	containers, err := dk.list(nil, true)
	assert.Nil(t, err)
	assert.Zero(t, len(containers))

	id1, err := dk.Run(RunOptions{Name: "name1"})
	assert.Nil(t, err)

	id2, err := dk.Run(RunOptions{Name: "name2"})
	assert.Nil(t, err)

	md.StopContainer(id2)

	containers, err = dk.List(nil)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(containers))
	assert.Equal(t, id1, containers[0].ID)

	containers, err = dk.list(nil, true)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(containers))
	assert.True(t, containers[0].ID == id2 || containers[1].ID == id2)

	md.InspectError = true
	containers, err = dk.List(nil)
	assert.Nil(t, err)
	assert.Zero(t, len(containers))
	md.InspectError = false

	running, err := dk.IsRunning("no")
	assert.Nil(t, err)
	assert.False(t, running)

	running, err = dk.IsRunning("name1")
	assert.Nil(t, err)
	assert.True(t, running)

	running, err = dk.IsRunning("name2")
	assert.Nil(t, err)
	assert.False(t, running)
}

func TestRunEnv(t *testing.T) {
	t.Parallel()
	_, dk := NewMock()

	env := map[string]string{
		"a": "b",
		"c": "d",
	}
	id, err := dk.Run(RunOptions{Name: "name1", Env: env})
	assert.Nil(t, err)

	container, err := dk.Get(id)
	assert.Nil(t, err)
	assert.Equal(t, env, container.Env)
}

func TestRemove(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	_, err := dk.Run(RunOptions{Name: "name1"})
	assert.Nil(t, err)

	id2, err := dk.Run(RunOptions{Name: "name2"})
	assert.Nil(t, err)

	md.ListError = true
	err = dk.Remove("name1")
	assert.NotNil(t, err)
	md.ListError = false

	md.RemoveError = true
	err = dk.Remove("name1")
	assert.NotNil(t, err)
	md.RemoveError = false

	err = dk.Remove("unknown")
	assert.NotNil(t, err)

	err = dk.Remove("name1")
	assert.Nil(t, err)

	err = dk.RemoveID(id2)
	assert.Nil(t, err)

	containers, err := dk.list(nil, true)
	assert.Nil(t, err)
	assert.Zero(t, len(containers))
}

func cacheKeys(cache map[string]time.Time) map[string]struct{} {
	res := map[string]struct{}{}
	for k := range cache {
		res[k] = struct{}{}
	}
	return res
}
