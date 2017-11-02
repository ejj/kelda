package scheduler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/vault"
	"github.com/kelda/kelda/minion/vault/mocks"
)

func TestRunWorker(t *testing.T) {
	mockVault := &mocks.SecretStore{}
	newVault = func(_ db.Conn) (vault.SecretStore, error) {
		return mockVault, nil
	}

	replaceFlows = func(ofcs []openflow.Container) error { return errors.New("err") }

	md, dk := docker.NewMock()
	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		container := view.InsertContainer()
		container.Image = "Image"
		container.Minion = "1.2.3.4"
		container.IP = "10.0.0.2"
		view.Commit(container)

		m := view.InsertMinion()
		m.Self = true
		m.PrivateIP = "1.2.3.4"
		view.Commit(m)

		etcd := view.InsertEtcd()
		etcd.LeaderIP = "leader"
		view.Commit(etcd)
		return nil
	})

	// Wrong Minion IP, should do nothing.
	runWorker(conn, dk, "1.2.3.5", "pubip")
	dkcs, err := dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 0)

	// Run with a list error, should do nothing.
	md.ListError = true
	runWorker(conn, dk, "1.2.3.4", "pubip")
	md.ListError = false
	dkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 0)

	// Success case. We should boot the container.
	runWorker(conn, dk, "1.2.3.4", "pubip")
	dkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 1)

	// The image should be as specified in the database.
	assert.Equal(t, "Image", dkcs[0].Image)

	// The running container's Docker ID and Status should be committed to the
	// database.
	dbc := conn.SelectFromContainer(nil)[0]
	assert.Equal(t, dkcs[0].ID, dbc.DockerID)
	assert.Equal(t, "Running", dbc.Status)

	// Change the container to require accessing secrets, but don't put
	// the secret into Vault yet.
	envKey := "envKey"
	secretName := "secretName"
	secretVal := "secretVal"
	conn.Txn(db.ContainerTable).Run(func(view db.Database) error {
		dbc := view.SelectFromContainer(nil)[0]
		dbc.Env = map[string]blueprint.ContainerValue{
			envKey: blueprint.NewSecret(secretName),
		}
		view.Commit(dbc)
		return nil
	})
	mockVault.On("Read", secretName).Return("", vault.ErrSecretDoesNotExist).Twice()
	runWorker(conn, dk, "1.2.3.4", "pubip")

	// The container's status should show that it's waiting for the secret to
	// be placed in Vault.
	dbc = conn.SelectFromContainer(nil)[0]
	assert.Equal(t, fmt.Sprintf("Waiting for secrets: [%s]", secretName), dbc.Status)

	// No containers should be running since there's only one container in the
	// database, and it's blocking on the secret.
	dkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 0)

	// Simulate placing the secret into Vault. Even though runWorker joins twice,
	// only one call is made to Vault because the first read was cached.
	mockVault.On("Read", secretName).Return(secretVal, nil).Once()
	runWorker(conn, dk, "1.2.3.4", "pubip")

	// The container's status should be updated.
	dbc = conn.SelectFromContainer(nil)[0]
	assert.Equal(t, "Running", dbc.Status)

	// The container should be booted, and running with the resolved secret value.
	dkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 1)
	assert.Equal(t, map[string]string{envKey: secretVal}, dkcs[0].Env)

	// The running container's ID should be committed to the database.
	assert.Equal(t, dkcs[0].ID, dbc.DockerID)

	// Check that all expected methods were called.
	mockVault.AssertExpectations(t)
}

func runSync(dk docker.Client, desiredContainers []evaluatedContainer,
	dkcs []docker.Container) []db.Container {

	changes, tdbcs, tdkcs := syncWorker(desiredContainers, dkcs)
	doContainers(dk, tdkcs, dockerKill)
	doContainers(dk, tdbcs, dockerRun)
	return changes
}

// This test verifies that the worker synchronizes with the database correctly,
// by booting or destroying containers as appropriate.
func TestSyncWorker(t *testing.T) {
	t.Parallel()

	md, dk := docker.NewMock()

	// The containers that should be running, according to the database. We
	// populate this manually here to simulate different states of the
	// database.
	evaluatedDbcs := []evaluatedContainer{
		{
			Container: db.Container{
				ID:      1,
				Image:   "Image1",
				Command: []string{"Cmd1"},
			},
			resolvedEnv: map[string]string{"Env": "1"},
		},
	}

	// Test when there are no containers running, but one specified in the
	// database. We should attempt to start the container, and there should be
	// no matching containers. However, the container never starts because
	// we mock an error when starting.
	md.StartError = true
	changed := runSync(dk, evaluatedDbcs, nil)
	md.StartError = false
	assert.Len(t, changed, 0)

	// The same case as above, except there is no error when starting, so the
	// container should actually get booted.
	runSync(dk, evaluatedDbcs, nil)

	// The previous test booted the desired container. Therefore, this sync
	// should pair the running container with the desired container.
	dkcs, err := dk.List(nil)
	changed, _, _ = syncWorker(evaluatedDbcs, dkcs)
	assert.NoError(t, err)

	if changed[0].DockerID != dkcs[0].ID {
		t.Error(spew.Sprintf("Incorrect DockerID: %v", changed))
	}

	// Assert that the pairing specified in `changed` is consistent with the
	// desired container in the database.
	evaluatedDbcs[0].DockerID = dkcs[0].ID
	evaluatedDbcs[0].Status = dkcs[0].Status
	assert.Len(t, changed, 1)
	assert.Equal(t, evaluatedDbcs[0].Container, changed[0])

	// Ensure that the booted container has the attributes specified in the
	// database.
	assert.Equal(t, evaluatedDbcs[0].Image, dkcs[0].Image)
	assert.Equal(t, evaluatedDbcs[0].Command, dkcs[0].Args)
	assert.Equal(t, evaluatedDbcs[0].resolvedEnv, dkcs[0].Env)

	// Unassign the DockerID, and run the sync again. Even though the DockerID
	// was unassigned, new containers shouldn't be booted.
	evaluatedDbcs[0].DockerID = ""
	changed = runSync(dk, evaluatedDbcs, dkcs)

	// Ensure that runSync did not boot any new containers.
	newDkcs, err := dk.List(nil)
	assert.NoError(t, err)
	assert.Equal(t, dkcs, newDkcs)

	// Assert that the pairing specified in `changed` is consistent with the
	// desired container in the database.
	evaluatedDbcs[0].DockerID = dkcs[0].ID
	evaluatedDbcs[0].Status = dkcs[0].Status
	assert.Len(t, changed, 1)
	assert.Equal(t, evaluatedDbcs[0].Container, changed[0])

	// Change the desired containers to be empty. Any running containers should
	// be stopped. However, the running container is not actually removed
	// because we mock an error during Remove.
	md.RemoveError = true
	changed = runSync(dk, nil, dkcs)
	md.RemoveError = false
	assert.Len(t, changed, 0)

	// Assert that the running containers has not changed.
	newDkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Equal(t, dkcs, newDkcs)

	// The same case as above, except don't throw an error when removing
	// containers. No containers should be running, and no containers should
	// be paired.
	changed = runSync(dk, nil, dkcs)
	assert.Len(t, changed, 0)

	dkcs, err = dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 0)
}

func TestInitsFiles(t *testing.T) {
	t.Parallel()

	md, dk := docker.NewMock()
	fileMap := map[string]string{"File": "Contents"}
	dbcs := []evaluatedContainer{
		{
			Container: db.Container{
				ID:    1,
				Image: "Image1",
			},
			resolvedFilepathToContent: fileMap,
		},
	}

	runSync(dk, dbcs, nil)
	dkcs, err := dk.List(nil)
	assert.NoError(t, err)
	assert.Len(t, dkcs, 1)
	assert.Equal(t, filesHash(fileMap), dkcs[0].Labels[filesKey])
	assert.Equal(t, map[docker.UploadToContainerOptions]struct{}{
		{
			ContainerID: dkcs[0].ID,
			UploadPath:  ".",
			TarPath:     "File",
			Contents:    "Contents",
		}: {},
	}, md.Uploads)
}

func TestSyncJoinScore(t *testing.T) {
	t.Parallel()

	dbc := evaluatedContainer{
		Container: db.Container{
			IP:       "1.2.3.4",
			Image:    "Image",
			Command:  []string{"cmd"},
			DockerID: "DockerID",
		},
		resolvedEnv:               map[string]string{"a": "b"},
		resolvedFilepathToContent: map[string]string{"c": "d"},
	}
	dkc := docker.Container{
		IP:    "1.2.3.4",
		Image: dbc.Image,
		Args:  dbc.Command,
		Env:   dbc.resolvedEnv,
		Labels: map[string]string{
			filesKey: filesHash(dbc.resolvedFilepathToContent),
		},
		ID: dbc.DockerID,
	}

	score := syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dbc.Image = "Image1"
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)

	dbc.Image = dkc.Image
	score = syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dbc.Command = []string{"wrong"}
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)

	dbc.Command = dkc.Args
	score = syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dbc.IP = "wrong"
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)

	dbc.IP = dkc.IP
	score = syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dbc.Command = dkc.Args
	dbc.resolvedEnv = map[string]string{"a": "wrong"}
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)
	dbc.resolvedEnv = dkc.Env

	dbc.resolvedFilepathToContent = map[string]string{"c": "wrong"}
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)

	dbc.resolvedFilepathToContent = map[string]string{"c": "d"}
	score = syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dkc.ImageID = "id"
	dbc.Command = dkc.Args
	dbc.resolvedEnv = dkc.Env
	dbc.ImageID = dkc.ImageID
	score = syncJoinScore(dbc, dkc)
	assert.Zero(t, score)

	dbc.ImageID = "wrong"
	score = syncJoinScore(dbc, dkc)
	assert.Equal(t, -1, score)
}

func TestOpenFlowContainers(t *testing.T) {
	conns := []db.Connection{
		{MinPort: 1, MaxPort: 1000},
		{MinPort: 2, MaxPort: 2, From: []string{blueprint.PublicInternetLabel},
			To: []string{"red"}},
		{MinPort: 3, MaxPort: 3, To: []string{blueprint.PublicInternetLabel},
			From: []string{"red"}},
		{MinPort: 4, MaxPort: 4, To: []string{blueprint.PublicInternetLabel},
			From: []string{"blue"}}}

	res := openflowContainers([]db.Container{
		{EndpointID: "f", IP: "1.2.3.4", Hostname: "red"}},
		conns)
	exp := []openflow.Container{{
		Veth:    "f",
		Patch:   "q_f",
		IP:      "1.2.3.4",
		Mac:     "02:00:01:02:03:04",
		ToPub:   map[int]struct{}{3: {}},
		FromPub: map[int]struct{}{2: {}},
	}}
	assert.Equal(t, exp, res)
}

func TestEvaluateContainerValues(t *testing.T) {
	t.Parallel()

	rawStringKey := "regular"
	rawStringVal := "string"
	pubIPKey := "pubIPKey"
	pubIP := "pubIP"
	secretKey := "secretKey"

	secretName := "secretName"
	secretVal := "secretVal"
	secretMap := map[string]string{
		secretName: secretVal,
	}

	// Test when all values are defined.
	input := map[string]blueprint.ContainerValue{
		rawStringKey: blueprint.NewString(rawStringVal),
		secretKey:    blueprint.NewSecret(secretName),
		pubIPKey:     blueprint.NewRuntimeValue(blueprint.ContainerPubIPKey),
	}
	resMap, resMissing := evaluateContainerValues(input, secretMap, pubIP)
	exp := map[string]string{
		rawStringKey: rawStringVal,
		secretKey:    secretVal,
		pubIPKey:     pubIP,
	}
	assert.Equal(t, exp, resMap)
	assert.Empty(t, resMissing)

	// Test when there is an undefined resource key. The undefined key should
	// be ignored, but the other values should be defined.
	input[pubIPKey] = blueprint.NewRuntimeValue("undefined")
	resMap, resMissing = evaluateContainerValues(input, secretMap, pubIP)
	delete(exp, pubIPKey)
	assert.Equal(t, exp, resMap)
	assert.Empty(t, resMissing)

	// Test when there is an undefined secret. It should be returned in the
	// `missing` list.
	undefinedSecretName := "undefined"
	_, resMissing = evaluateContainerValues(map[string]blueprint.ContainerValue{
		secretKey: blueprint.NewSecret(undefinedSecretName),
	}, secretMap, pubIP)
	assert.Equal(t, []string{undefinedSecretName}, resMissing)
}
