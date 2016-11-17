package docker

import (
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
)

func TestPull(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.PullError = true
	if err := dk.Pull("foo"); err == nil {
		t.Errorf("Expected error")
	}
	if _, ok := dk.imageCache["foo"]; ok {
		t.Errorf("Unexpected image cache entry foo")
	}
	md.PullError = false

	if err := dk.Pull("foo"); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	exp := map[string]struct{}{
		"foo": {},
	}
	if !reflect.DeepEqual(md.Pulled, exp) {
		t.Error(spew.Sprintf("Pulled %v\nexpected: %v", md.Pulled, exp))

	}
	if !reflect.DeepEqual(cacheKeys(dk.imageCache), exp) {
		t.Error(spew.Sprintf("Pulled %v\nexpected: %v", md.Pulled, exp))

	}

	if err := dk.Pull("foo"); err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if !reflect.DeepEqual(md.Pulled, exp) {
		t.Error(spew.Sprintf("Pulled %v\nexpected: %v", md.Pulled, exp))

	}
	if !reflect.DeepEqual(cacheKeys(dk.imageCache), exp) {
		t.Error(spew.Sprintf("Pulled %v\nexpected: %v", md.Pulled, exp))

	}
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
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
		return
	}
	if !cached {
		t.Errorf("Should not have pulled the image because it's cached.")
	}
}

func TestPullImageNotCached(t *testing.T) {
	pullCacheTimeout = 300 * time.Millisecond

	cached, err := checkCache(func() {
		time.Sleep(500 * time.Millisecond)
	})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
		return
	}
	if cached {
		t.Errorf("Should have pulled the image because it's no longer cached.")
	}
}

func TestCreateGet(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.PullError = true
	if _, err := dk.create("name", "image", nil, nil, nil, nil); err == nil {
		t.Error("Expected error")
	}
	md.PullError = false

	md.CreateError = true
	if _, err := dk.create("name", "image", nil, nil, nil, nil); err == nil {
		t.Error("Expected error")
	}
	md.CreateError = false

	if _, err := dk.Get("awef"); err == nil {
		t.Error("Expected error")
	}

	args := []string{"arg1"}
	env := map[string]struct{}{
		"envA=B": {},
	}
	labels := map[string]string{"label": "foo"}
	id, err := dk.create("name", "image", args, labels, env, nil)
	if err != nil {
		t.Error("Unexpected error")
	}

	container, err := dk.Get(id)
	if err != nil {
		t.Error("Unexpected error")
	}

	expContainer := Container{
		Name:   "name",
		ID:     id,
		Image:  "image",
		Args:   args,
		Env:    map[string]string{"envA": "B"},
		Labels: labels,
	}

	if !reflect.DeepEqual(container, expContainer) {
		t.Error(spew.Sprintf("containers %v\nexpected %v\n",
			container, expContainer))
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	md.CreateError = true
	if _, err := dk.Run(RunOptions{}); err == nil {
		t.Error("Expected Error")
	}
	md.CreateError = false

	md.StartError = true
	if _, err := dk.Run(RunOptions{}); err == nil {
		t.Error("Expected Error")
	}
	md.StartError = false

	md.ListError = true
	if _, err := dk.List(nil); err == nil {
		t.Error("Expected Error")
	}
	md.ListError = false

	md.ListError = true
	if _, err := dk.IsRunning("foo"); err == nil {
		t.Error("Expected Error")
	}
	md.ListError = false

	containers, err := dk.list(nil, true)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if len(containers) > 0 {
		t.Errorf(spew.Sprintf("Unexpected containers: %v", containers))
	}

	id1, err := dk.Run(RunOptions{Name: "name1"})
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	id2, err := dk.Run(RunOptions{Name: "name2"})
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	md.StopContainer(id2)

	containers, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if len(containers) != 1 || containers[0].ID != id1 {
		t.Errorf(spew.Sprintf("Unexpected containers: %v", containers))
	}

	containers, err = dk.list(nil, true)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	if len(containers) != 2 ||
		(containers[0].ID != id2 && containers[1].ID != id2) {
		t.Errorf(spew.Sprintf("Unexpected containers: %v", containers))
	}

	md.InspectError = true
	containers, err = dk.List(nil)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if len(containers) > 0 {
		t.Errorf(spew.Sprintf("Unexpected containers: %v", containers))
	}
	md.InspectError = false

	running, err := dk.IsRunning("no")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if running {
		t.Error("Container should not be running")
	}

	running, err = dk.IsRunning("name1")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if !running {
		t.Error("Container should be running")
	}

	running, err = dk.IsRunning("name2")
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if running {
		t.Error("Container should not be running")
	}
}

func TestRunEnv(t *testing.T) {
	t.Parallel()
	_, dk := NewMock()

	env := map[string]string{
		"a": "b",
		"c": "d",
	}
	id, err := dk.Run(RunOptions{Name: "name1", Env: env})
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	container, err := dk.Get(id)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	if !reflect.DeepEqual(container.Env, env) {
		t.Errorf(spew.Sprintf("Got: %v\nExp: %v\n", container.Env, env))
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	_, err := dk.Run(RunOptions{Name: "name1"})
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	id2, err := dk.Run(RunOptions{Name: "name2"})
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}

	md.ListError = true
	if err := dk.Remove("name1"); err == nil {
		t.Error("Expected Error")
	}
	md.ListError = false

	md.RemoveError = true
	if err := dk.Remove("name1"); err == nil {
		t.Error("Expected Error")
	}
	md.RemoveError = false

	if err := dk.Remove("unknown"); err == nil {
		t.Error("Expected Error")
	}

	if err := dk.Remove("name1"); err != nil {
		t.Error("Unexpected Error")
	}

	if err := dk.RemoveID(id2); err != nil {
		t.Errorf("Unexpected Error: %v", err)
	}

	containers, err := dk.list(nil, true)
	if err != nil {
		t.Errorf("Unexpected Error: %v", err)
	}

	if len(containers) > 0 {
		t.Errorf(spew.Sprintf("Unexpected containers: %v", containers))
	}
}

func cacheKeys(cache map[string]time.Time) map[string]struct{} {
	res := map[string]struct{}{}
	for k := range cache {
		res[k] = struct{}{}
	}
	return res
}
