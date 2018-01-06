package client

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/kelda/kelda/api/pb"
	"github.com/kelda/kelda/db"
)

type mockAPIClient struct {
	mockResponse string
	mockError    error
}

func (c mockAPIClient) Query(ctx context.Context, in *pb.DBQuery,
	opts ...grpc.CallOption) (*pb.QueryReply, error) {

	return &pb.QueryReply{TableContents: c.mockResponse}, c.mockError
}

func (c mockAPIClient) Deploy(ctx context.Context, in *pb.DeployRequest,
	opts ...grpc.CallOption) (*pb.DeployReply, error) {

	return &pb.DeployReply{}, nil
}

func (c mockAPIClient) QueryCounters(ctx context.Context, in *pb.CountersRequest,
	opts ...grpc.CallOption) (*pb.CountersReply, error) {

	return &pb.CountersReply{}, nil
}

func (c mockAPIClient) QueryMinionCounters(ctx context.Context, in *pb.
	MinionCountersRequest, opts ...grpc.CallOption) (*pb.CountersReply, error) {

	return &pb.CountersReply{}, nil
}

func (c mockAPIClient) Version(ctx context.Context, in *pb.VersionRequest,
	opts ...grpc.CallOption) (*pb.VersionReply, error) {

	return &pb.VersionReply{}, nil
}

func (c mockAPIClient) SetSecret(ctx context.Context, in *pb.Secret,
	opts ...grpc.CallOption) (*pb.SecretReply, error) {

	return &pb.SecretReply{}, nil
}

func TestUnmarshalMachine(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1,"Role":"Master","Provider":"Amazon",` +
			`"Region":"","Size":"size","DiskSize":0,"SSHKeys":null,` +
			`"CloudID":"","PublicIP":"8.8.8.8","PrivateIP":"9.9.9.9"}]`,
	}
	c := clientImpl{pbClient: apiClient}
	res, err := c.QueryMachines()
	assert.NoError(t, err)

	exp := []db.Machine{
		{
			ID:        1,
			Role:      db.Master,
			Provider:  db.Amazon,
			Size:      "size",
			PublicIP:  "8.8.8.8",
			PrivateIP: "9.9.9.9",
		},
	}
	assert.Equal(t, exp, res)
}

func TestUnmarshalContainer(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1,"Pid":0,"IP":"","Mac":"","Minion":"",` +
			`"PodName":"pod-name","BlueprintID":"","Image":"image",` +
			`"Command":["cmd","arg"],"Env":null}]`,
	}
	c := clientImpl{pbClient: apiClient}
	res, err := c.QueryContainers()
	assert.NoError(t, err)

	exp := []db.Container{
		{
			PodName: "pod-name",
			Image:   "image",
			Command: []string{"cmd", "arg"},
		},
	}
	assert.Equal(t, exp, res)
}

func TestUnmarshalImage(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1,"Name":"foo","Dockerfile":"bar",` +
			`"RepoDigest":"","Status":"building"}]`,
	}
	c := clientImpl{pbClient: apiClient}
	res, err := c.QueryImages()
	assert.NoError(t, err)
	assert.Equal(t, []db.Image{
		{ID: 1, Name: "foo", Dockerfile: "bar", Status: "building"},
	}, res)
}

func TestUnmarshalError(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1`,
	}
	c := clientImpl{pbClient: apiClient}

	_, err := c.QueryMachines()
	assert.EqualError(t, err, "unexpected end of JSON input")
}

func TestGrpcError(t *testing.T) {
	t.Parallel()

	exp := errors.New("timeout")
	apiClient := mockAPIClient{
		mockError: exp,
	}
	c := clientImpl{pbClient: apiClient}

	_, err := c.QueryMachines()
	assert.EqualError(t, err, "timeout")
}
