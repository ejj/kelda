package command

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/quilt/quilt/api"
	clientMock "github.com/quilt/quilt/api/client/mocks"
	"github.com/quilt/quilt/db"
)

func TestPsFlags(t *testing.T) {
	t.Parallel()

	expHost := "IP"

	cmd := NewPsCommand()
	err := parseHelper(cmd, []string{"-H", expHost})

	assert.NoError(t, err)
	assert.Equal(t, expHost, cmd.common.host)
}

func TestPsErrors(t *testing.T) {
	t.Parallel()

	var cmd *Ps
	var mockGetter *clientMock.Getter
	var mockClient, mockLeaderClient *clientMock.Client

	mockErr := errors.New("error")

	// Error connecting to local client
	mockGetter = new(clientMock.Getter)
	mockGetter.On("Client", mock.Anything).Return(nil, mockErr)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.EqualError(t, cmd.run(), "error connecting to quilt daemon: error")
	mockGetter.AssertExpectations(t)

	// Error querying machines
	mockGetter = new(clientMock.Getter)
	mockClient = &clientMock.Client{MachineErr: mockErr}
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(nil, mockErr)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.EqualError(t, cmd.run(), "unable to query machines: error")
	mockGetter.AssertExpectations(t)

	// Error connecting to leader
	mockGetter = new(clientMock.Getter)
	mockClient = new(clientMock.Client)
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(nil, mockErr)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.NoError(t, cmd.run())
	mockGetter.AssertExpectations(t)

	// Error querying containers
	mockGetter = new(clientMock.Getter)
	mockClient = new(clientMock.Client)
	mockLeaderClient = &clientMock.Client{ContainerErr: mockErr}
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(mockLeaderClient, nil)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.EqualError(t, cmd.run(), "unable to query containers: error")
	mockGetter.AssertExpectations(t)

	// Error querying connections from LeaderClient
	mockGetter = new(clientMock.Getter)
	mockClient = new(clientMock.Client)
	mockLeaderClient = &clientMock.Client{ConnectionErr: mockErr}
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(mockLeaderClient, nil)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.EqualError(t, cmd.run(), "unable to query connections: error")
	mockGetter.AssertExpectations(t)

	// Error querying containers in queryWorkers(), but fine for Leader.
	mockGetter = new(clientMock.Getter)
	mockClient = &clientMock.Client{
		MachineReturn: []db.Machine{
			{
				PublicIP: "1.2.3.4",
				Role:     db.Worker,
			},
		},
		ContainerErr: mockErr,
	}
	mockLeaderClient = new(clientMock.Client)
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(mockLeaderClient, nil)

	cmd = &Ps{&commonFlags{}, mockGetter}
	assert.Equal(t, 0, cmd.Run())
	mockGetter.AssertExpectations(t)
}

func TestPsSuccess(t *testing.T) {
	t.Parallel()

	mockGetter := new(clientMock.Getter)
	mockClient := new(clientMock.Client)
	mockLeaderClient := new(clientMock.Client)

	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)
	mockGetter.On("LeaderClient", mock.Anything).Return(mockLeaderClient, nil)

	cmd := &Ps{&commonFlags{}, mockGetter}
	assert.Equal(t, 0, cmd.Run())
	mockGetter.AssertExpectations(t)
}

func TestQueryWorkersSuccess(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			StitchID: "1",
		},
	}

	machines := []db.Machine{
		{
			PublicIP: "1.2.3.4",
			Role:     db.Worker,
		},
	}

	mockGetter := new(clientMock.Getter)
	mockClient := &clientMock.Client{
		ContainerReturn: containers,
	}
	mockGetter.On("Client", mock.Anything).Return(mockClient, nil)

	cmd := &Ps{&commonFlags{}, mockGetter}
	result := cmd.queryWorkers(machines)
	assert.Equal(t, containers, result)
	mockGetter.AssertExpectations(t)
}

func TestQueryWorkersFailure(t *testing.T) {
	t.Parallel()

	containers := []db.Container{
		{
			StitchID: "1",
		},
	}

	machines := []db.Machine{
		{
			PublicIP: "1.2.3.4",
			Role:     db.Worker,
		},
		{
			PublicIP: "5.6.7.8",
			Role:     db.Worker,
		},
	}

	mockErr := errors.New("error")

	// Getting Worker Machine Client fails. Still query non-failing machine.
	mockClient := &clientMock.Client{
		ContainerReturn: containers,
	}
	mockGetter := new(clientMock.Getter)
	mockGetter.On("Client", api.RemoteAddress("1.2.3.4")).Return(nil, mockErr)
	mockGetter.On("Client", api.RemoteAddress("5.6.7.8")).Return(mockClient, nil)

	cmd := &Ps{&commonFlags{}, mockGetter}
	result := cmd.queryWorkers(machines)
	assert.Equal(t, containers, result)
	mockGetter.AssertExpectations(t)

	// Worker Machine client throws error.
	// Still get container from non-failing machine.
	mockGetter = new(clientMock.Getter)
	failingClient := &clientMock.Client{
		ContainerErr: mockErr,
	}
	mockGetter.On("Client", api.RemoteAddress("1.2.3.4")).Return(failingClient, nil)
	mockGetter.On("Client", api.RemoteAddress("5.6.7.8")).Return(mockClient, nil)

	cmd = &Ps{&commonFlags{}, mockGetter}
	result = cmd.queryWorkers(machines)
	assert.Equal(t, containers, result)
	mockGetter.AssertExpectations(t)
}

func TestUpdateContainers(t *testing.T) {
	t.Parallel()

	created := time.Now()

	lContainers := []db.Container{
		{
			StitchID: "1",
		},
	}

	wContainers := []db.Container{
		{
			StitchID: "1",
			Created:  created,
		},
	}

	// Test update a matching container.
	expect := wContainers
	result := updateContainers(lContainers, wContainers)
	assert.Equal(t, expect, result)

	// Test container in leader, not in worker.
	newContainer := db.Container{
		StitchID: "2",
	}
	lContainers = append(lContainers, newContainer)
	expect = append(expect, newContainer)
	result = updateContainers(lContainers, wContainers)
	assert.Equal(t, expect, result)

	// Test if lContainers empty.
	lContainers = []db.Container{}
	expect = wContainers
	result = updateContainers(lContainers, wContainers)
	assert.Equal(t, expect, result)

	// Test if wContainers empty.
	lContainers = wContainers
	wContainers = []db.Container{}
	expect = lContainers
	result = updateContainers(lContainers, wContainers)
	assert.Equal(t, expect, result)

	// Test if both empty.
	lContainers = []db.Container{}
	expect = []db.Container{}
	result = updateContainers(lContainers, wContainers)
	assert.Equal(t, expect, result)
}
