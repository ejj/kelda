package server

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/net/context"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/api/client/mocks"
	"github.com/kelda/kelda/api/pb"
	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/kubernetes"
	kubeMocks "github.com/kelda/kelda/minion/kubernetes/mocks"
	"github.com/stretchr/testify/assert"
)

func checkQuery(t *testing.T, s server, table db.TableType, exp string) {
	reply, err := s.Query(context.Background(),
		&pb.DBQuery{Table: string(table)})

	assert.NoError(t, err)
	assert.Equal(t, exp, reply.TableContents, "Wrong query response")
}

func TestQueryErrors(t *testing.T) {
	// Invalid table type.
	_, err := server{}.Query(context.Background(),
		&pb.DBQuery{Table: string(db.HostnameTable)})
	assert.EqualError(t, err, "unrecognized table: db.Hostname")

	// Error getting the leader client.
	newLeaderClient = func(_ []db.Machine, _ connection.Credentials) (
		client.Client, error) {
		return nil, errors.New("get leader error")
	}
	s := server{db.New(), true, nil}
	_, err = s.Query(context.Background(),
		&pb.DBQuery{Table: string(db.ContainerTable)})
	assert.EqualError(t, err, "get leader error")
}

func TestQueryMachinesDaemon(t *testing.T) {
	t.Parallel()

	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		m := view.InsertMachine()
		m.Role = db.Master
		m.Provider = db.Amazon
		m.Size = "size"
		m.PublicIP = "8.8.8.8"
		m.PrivateIP = "9.9.9.9"
		m.Status = db.Connected
		view.Commit(m)

		return nil
	})

	exp := `[{"ID":1,"Role":"Master","Provider":"Amazon",` +
		`"Region":"","Size":"size","DiskSize":0,"SSHKeys":null,"FloatingIP":"",` +
		`"Preemptible":false,"CloudID":"","PublicIP":"8.8.8.8",` +
		`"PrivateIP":"9.9.9.9","Status":"connected"}]`

	checkQuery(t, server{conn, true, nil}, db.MachineTable, exp)
}

func TestQueryContainersCluster(t *testing.T) {
	t.Parallel()

	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		c := view.InsertContainer()
		c.PodName = "podName"
		c.Image = "image"
		c.Command = []string{"cmd", "arg"}
		view.Commit(c)

		return nil
	})

	exp := `[{"PodName":"podName","Command":["cmd","arg"],` +
		`"Created":"0001-01-01T00:00:00Z","Image":"image"}]`

	checkQuery(t, server{conn, false, nil}, db.ContainerTable, exp)
}

func TestQueryContainersDaemon(t *testing.T) {
	newLeaderClient = func(_ []db.Machine, _ connection.Credentials) (
		client.Client, error) {
		mc := new(mocks.Client)
		mc.On("QueryContainers").Return([]db.Container{{
			BlueprintID: "id",
			Image:       "image",
		}, {
			BlueprintID: "id2",
			Image:       "image2",
		}}, nil)
		mc.On("Close").Return(nil)
		return mc, nil
	}

	exp := `[{"BlueprintID":"id","Created":"0001-01-01T00:00:00Z",` +
		`"Image":"image"},{"BlueprintID":"id2",` +
		`"Created":"0001-01-01T00:00:00Z","Image":"image2"}]`
	checkQuery(t, server{db.New(), true, nil}, db.ContainerTable, exp)
}

func TestBadDeployment(t *testing.T) {
	conn := db.New()
	s := server{conn: conn, runningOnDaemon: true}

	badDeployment := `{`

	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: badDeployment})

	assert.EqualError(t, err,
		"unable to parse blueprint: unexpected end of JSON input")
}
func TestInvalidImage(t *testing.T) {
	conn := db.New()
	s := server{conn: conn, runningOnDaemon: true}
	testInvalidImage(t, s, "has:morethan:two:colons",
		"could not parse container image has:morethan:two:colons: "+
			"invalid reference format")
	testInvalidImage(t, s, "has-empty-tag:",
		"could not parse container image has-empty-tag:: "+
			"invalid reference format")
	testInvalidImage(t, s, "has-empty-tag::digest",
		"could not parse container image has-empty-tag::digest: "+
			"invalid reference format")
	testInvalidImage(t, s, "hasCapital",
		"could not parse container image hasCapital: "+
			"invalid reference format: repository name must be lowercase")
}

func testInvalidImage(t *testing.T, s server, img, expErr string) {
	deployment := fmt.Sprintf(`
	{"Containers":[
		{"ID": "1",
                "Image": {"Name": "%s"},
                "Command":[
                        "sleep",
                        "10000"
                ],
                "Env": {}
	}]}`, img)

	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: deployment})
	assert.EqualError(t, err, expErr)
}

func TestDeploy(t *testing.T) {
	conn := db.New()
	s := server{conn: conn, runningOnDaemon: true}

	createMachineDeployment := `
	{"Machines":[
		{"Provider":"Amazon",
		"Role":"Master",
		"Size":"m4.large",
		"Region":"us-west-1"
	}, {"Provider":"Amazon",
		"Role":"Worker",
		"Size":"m4.large",
		"Region":"us-west-1"
	}]}`

	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: createMachineDeployment})

	assert.NoError(t, err)

	var bp db.Blueprint
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		bp, err = view.GetBlueprint()
		assert.NoError(t, err)
		return nil
	})

	exp, err := blueprint.FromJSON(createMachineDeployment)
	assert.NoError(t, err)
	assert.Equal(t, exp, bp.Blueprint)
}

func TestDeployUnsupportedRegion(t *testing.T) {
	conn := db.New()
	s := server{conn: conn, runningOnDaemon: true}

	createMachineDeployment := `
	{"Machines":[
		{"Provider":"Amazon",
		"Role":"Master",
		"Size":"m4.large",
		"Region":"FakeRegion"
	}, {"Provider":"Amazon",
		"Role":"Worker",
		"Size":"m4.large",
		"Region":"FakeRegion"
	}]}`

	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: createMachineDeployment})

	assert.EqualError(t, err, "region: FakeRegion is not supported "+
		"for provider: Amazon")
}

func TestDeployChangeNamespace(t *testing.T) {
	t.Parallel()

	conn := db.New()
	conn.Txn(db.BlueprintTable, db.MachineTable).Run(func(view db.Database) error {
		bp := view.InsertBlueprint()
		bp.Namespace = "old"
		view.Commit(bp)

		dbm := view.InsertMachine()
		view.Commit(dbm)
		return nil
	})
	s := server{conn: conn, runningOnDaemon: true}

	newNamespaceBlueprint := `{"Namespace":"new"}`
	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: newNamespaceBlueprint})
	assert.NoError(t, err)

	// The machines in the database should be removed.
	assert.Empty(t, conn.SelectFromMachine(nil))
}

func TestVagrantDeployment(t *testing.T) {
	conn := db.New()
	s := server{conn: conn, runningOnDaemon: true}

	vagrantDeployment := `
	{"Machines":[
		{"Provider":"Vagrant",
		"Role":"Master",
		"Size":"m4.large"
	}, {"Provider":"Vagrant",
		"Role":"Worker",
		"Size":"m4.large"
	}]}`
	vagrantErrMsg := "The Vagrant provider is still in development." +
		" The blueprint will continue to run, but" +
		" there may be some errors."

	_, err := s.Deploy(context.Background(),
		&pb.DeployRequest{Deployment: vagrantDeployment})

	assert.Error(t, err, vagrantErrMsg)

	var bp db.Blueprint
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		bp, err = view.GetBlueprint()
		assert.NoError(t, err)
		return nil
	})

	exp, err := blueprint.FromJSON(vagrantDeployment)
	assert.NoError(t, err)
	assert.Equal(t, exp, bp.Blueprint)
}

func TestDaemonOnlyEndpoints(t *testing.T) {
	t.Parallel()

	_, err := server{runningOnDaemon: false}.QueryMinionCounters(nil, nil)
	assert.EqualError(t, err, errDaemonOnlyRPC.Error())

	_, err = server{runningOnDaemon: false}.Deploy(nil, nil)
	assert.EqualError(t, err, errDaemonOnlyRPC.Error())
}

func TestQueryImagesCluster(t *testing.T) {
	t.Parallel()

	conn := db.New()
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		img := view.InsertImage()
		img.Name = "foo"
		view.Commit(img)

		return nil
	})

	exp := `[{"ID":1,"Name":"foo","Dockerfile":"","RepoDigest":"","Status":""}]`
	checkQuery(t, server{conn, false, nil}, db.ImageTable, exp)
}

func TestQueryImagesDaemon(t *testing.T) {
	newLeaderClient = func(_ []db.Machine, _ connection.Credentials) (
		client.Client, error) {
		mc := new(mocks.Client)
		mc.On("QueryImages").Return([]db.Image{{
			Name: "bar",
		}}, nil)
		mc.On("Close").Return(nil)
		return mc, nil
	}

	exp := `[{"ID":0,"Name":"bar","Dockerfile":"","RepoDigest":"","Status":""}]`
	checkQuery(t, server{db.New(), true, nil}, db.ImageTable, exp)
}

// The Daemon should get a connection to the leader of the cluster, and
// forward the secret association.
func TestSetSecretDaemon(t *testing.T) {
	secretName := "secretName"
	secretValue := "secretValue"

	mc := new(mocks.Client)
	mc.On("SetSecret", secretName, secretValue).Return(nil)
	mc.On("Close").Return(nil)
	newLeaderClient = func(_ []db.Machine, _ connection.Credentials) (
		client.Client, error) {
		return mc, nil
	}

	_, err := server{db.New(), true, nil}.SetSecret(nil, &pb.Secret{
		Name: secretName, Value: secretValue,
	})
	assert.NoError(t, err)
	mc.AssertExpectations(t)
}

// The minion should get a connection to the Kubernetes secret client and write
// the secret.
func TestSetSecretCluster(t *testing.T) {
	secretName := "secretName"
	secretValue := "secretValue"

	mockClient := &kubeMocks.SecretClient{}
	newSecretClient = func() (kubernetes.SecretClient, error) {
		return mockClient, nil
	}

	mockClient.On("Set", secretName, secretValue).Return(nil).Once()
	_, err := server{db.New(), false, nil}.SetSecret(nil, &pb.Secret{
		Name: secretName, Value: secretValue,
	})
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestSetSecretClusterError(t *testing.T) {
	newSecretClient = func() (kubernetes.SecretClient, error) {
		return nil, assert.AnError
	}

	_, err := server{db.New(), false, nil}.SetSecret(nil, &pb.Secret{})
	assert.NotNil(t, err)
}
