package docker

import (
	"testing"
	"time"

	dkc "github.com/fsouza/go-dockerclient"
	"github.com/kelda/kelda/minion/ipdef"
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
		"foo:latest": {},
	}
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))

	err = dk.Pull("foo")
	assert.Nil(t, err)
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))

	err = dk.Pull("bar")
	assert.Nil(t, err)

	exp = map[string]struct{}{
		"foo:latest": {},
		"bar:latest": {},
	}
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))

	err = dk.Pull("bar:tag")
	assert.Nil(t, err)

	exp = map[string]struct{}{
		"foo:latest": {},
		"bar:latest": {},
		"bar:tag":    {},
	}
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))

	err = dk.Pull("bar:tag2@sha256:asdfasdfasdfasdf")
	assert.Nil(t, err)

	exp = map[string]struct{}{
		"foo:latest": {},
		"bar:latest": {},
		"bar:tag":    {},
		"bar:tag2":   {},
	}
	assert.Equal(t, exp, md.Pulled)
	assert.Equal(t, exp, cacheKeys(dk.imageCache))
}

func checkCache(prePull func()) (bool, error) {
	testImage := "foo"
	md, dk := NewMock()

	if err := dk.Pull(testImage); err != nil {
		return false, err
	}

	delete(md.Pulled, testImage+":latest")

	prePull()
	if err := dk.Pull(testImage + ":latest"); err != nil {
		return false, err
	}

	_, pulled := md.Pulled[testImage+":latest"]
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
	_, err := dk.create("name", "image", "hostname", nil, nil, nil, nil, nil, nil)
	assert.NotNil(t, err)
	md.PullError = false

	md.CreateError = true
	_, err = dk.create("name", "image", "hostname", nil, nil, nil, nil, nil, nil)
	assert.NotNil(t, err)
	md.CreateError = false

	_, err = dk.Get("awef")
	assert.NotNil(t, err)

	args := []string{"arg1"}
	env := []string{"envA=B"}
	labels := map[string]string{"label": "foo"}
	id, err := dk.create("name", "image", "hostname", args, labels, env, nil,
		nil, nil)
	assert.Nil(t, err)

	container, err := dk.Get(id)
	assert.Nil(t, err)

	expContainer := Container{
		Name:     "name",
		ID:       id,
		Image:    "image",
		Args:     args,
		Env:      map[string]string{"envA": "B"},
		Labels:   labels,
		Hostname: "hostname",
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

	id1, err := dk.Run(RunOptions{Name: "name1", IP: "1.2.3.4"})
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

	md.InspectContainerError = true
	containers, err = dk.List(nil)
	assert.Nil(t, err)
	assert.Zero(t, len(containers))
	md.InspectContainerError = false

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

	testEnvs := []map[string]string{
		{
			"a": "b",
			"c": "d",
		},
		{
			"a": "",
		},
		{
			"a": "has=equal",
		},
		{
			"a": "has=two=equals",
		},
	}

	for _, env := range testEnvs {
		id, err := dk.Run(RunOptions{
			Name: "name",
			Env:  env,
		})
		assert.NoError(t, err)

		actual, err := dk.Get(id)
		assert.NoError(t, err)
		assert.Equal(t, env, actual.Env)
	}
}

func TestRunFilepathToContent(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	fileMap := map[string]string{
		"foo":      "bar",
		"/baz":     "qux",
		"a/b/c/d":  "e",
		"/a/b/c/d": "e",
		"../a":     "b",
	}
	id, err := dk.Run(RunOptions{Name: "name1", FilepathToContent: fileMap})
	assert.NoError(t, err)

	assert.Equal(t, map[UploadToContainerOptions]struct{}{
		{ContainerID: id, UploadPath: ".", TarPath: "foo", Contents: "bar"}:   {},
		{ContainerID: id, UploadPath: "/", TarPath: "baz", Contents: "qux"}:   {},
		{ContainerID: id, UploadPath: ".", TarPath: "a/b/c/d", Contents: "e"}: {},
		{ContainerID: id, UploadPath: "/", TarPath: "a/b/c/d", Contents: "e"}: {},
		{ContainerID: id, UploadPath: ".", TarPath: "../a", Contents: "b"}:    {},
	}, md.Uploads)

	md.UploadError = true
	_, err = dk.Run(RunOptions{Name: "name1", FilepathToContent: fileMap})
	assert.NotNil(t, err)
}

func TestConfigureNetwork(t *testing.T) {
	md, dk := NewMock()

	err := dk.ConfigureNetwork("kelda")
	assert.NoError(t, err)

	exp := &dkc.Network{
		Name:   "kelda",
		Driver: "kelda",
		IPAM: dkc.IPAMOptions{
			Config: []dkc.IPAMConfig{{
				Subnet:  ipdef.KeldaSubnet.String(),
				Gateway: ipdef.GatewayIP.String()}}}}
	assert.Equal(t, exp, md.Networks["kelda"])

	md.CreateNetworkError = true
	err = dk.ConfigureNetwork("kelda")
	assert.NoError(t, err)
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

func TestBuild(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	err := dk.Build("foo", "bar", false)
	assert.NoError(t, err)
	assert.Equal(t, map[BuildImageOptions]struct{}{
		{
			Name:       "foo",
			Dockerfile: "bar",
			NoCache:    true,
		}: {},
	}, md.Built)

	md.BuildError = true
	err = dk.Build("foo", "bar", false)
	assert.NotNil(t, err)
}

func TestPush(t *testing.T) {
	t.Parallel()
	md, dk := NewMock()

	err := dk.Build("bar:baz", "dockerfile", false)
	assert.NoError(t, err)

	repoDigest, err := dk.Push("foo", "bar:baz")
	assert.NotEmpty(t, repoDigest)
	assert.NoError(t, err)
	assert.Equal(t, map[dkc.PushImageOptions]struct{}{
		{
			Registry: "foo",
			Name:     "bar",
			Tag:      "baz",
		}: {},
	}, md.Pushed)

	md.PushError = true
	_, err = dk.Push("foo", "bar")
	assert.NotNil(t, err)
}

func cacheKeys(cache map[string]*cacheEntry) map[string]struct{} {
	res := map[string]struct{}{}
	for k := range cache {
		res[k] = struct{}{}
	}
	return res
}
