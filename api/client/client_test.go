package client

import (
	"errors"
	"reflect"
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/quilt/quilt/api/pb"
	"github.com/quilt/quilt/db"
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

func TestUnmarshalMachine(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1,"Role":"Master","Provider":"Amazon",` +
			`"Region":"","Size":"size","DiskSize":0,"SSHKeys":null,` +
			`"CloudID":"","PublicIP":"8.8.8.8","PrivateIP":"9.9.9.9"}]`,
	}
	c := clientImpl{pbClient: apiClient}
	res, err := c.QueryMachines()
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
		return
	}

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

	if !reflect.DeepEqual(exp, res) {
		t.Errorf("Bad unmarshalling of machines: expected %v, got %v.",
			exp, res)
	}
}

func TestUnmarshalContainer(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1,"Pid":0,"IP":"","Mac":"","Minion":"",` +
			`"DockerID":"docker-id","StitchID":"","Image":"image",` +
			`"Command":["cmd","arg"],"Labels":["labelA","labelB"],` +
			`"Env":null}]`,
	}
	c := clientImpl{pbClient: apiClient}
	res, err := c.QueryContainers()
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
		return
	}

	exp := []db.Container{
		{
			DockerID: "docker-id",
			Image:    "image",
			Command:  []string{"cmd", "arg"},
			Labels:   []string{"labelA", "labelB"},
		},
	}

	if !reflect.DeepEqual(exp, res) {
		t.Errorf("Bad unmarshalling of containers: expected %v, got %v.",
			exp, res)
	}
}

func TestUnmarshalError(t *testing.T) {
	t.Parallel()

	apiClient := mockAPIClient{
		mockResponse: `[{"ID":1`,
	}
	c := clientImpl{pbClient: apiClient}

	_, err := c.QueryMachines()
	if err == nil {
		t.Error("Bad json should throw a unmarshalling error, but got nothing")
	}

	exp := "unexpected end of JSON input"
	if err.Error() != exp {
		t.Errorf("Bad json should throw a unmarshalling error:"+
			"expected %s, got %s", exp, err.Error())
	}
}

func TestGrpcError(t *testing.T) {
	t.Parallel()

	exp := errors.New("timeout")
	apiClient := mockAPIClient{
		mockError: exp,
	}
	c := clientImpl{pbClient: apiClient}

	_, err := c.QueryMachines()
	if err == nil {
		t.Error("`Query` should have returned grpc errors, but got nothing")
	}
	if err.Error() != exp.Error() {
		t.Errorf("`Query` should returned grpc errors: expected %s, got %s",
			exp.Error(), err.Error())
	}
}
