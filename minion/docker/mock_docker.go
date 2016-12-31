package docker

import (
	"errors"
	"strings"
	"sync"

	dkc "github.com/fsouza/go-dockerclient"
	"github.com/satori/go.uuid"
)

type mockContainer struct {
	*dkc.Container
	Running bool
}

// MockClient gives unit testers access to the internals of the mock docker client
// returned by NewMock.
type MockClient struct {
	*sync.Mutex
	Pulled     map[string]struct{}
	Containers map[string]mockContainer
	Networks   map[string]*dkc.Network

	createdExecs map[string]dkc.CreateExecOptions
	Executions   map[string][]string

	CreateError     bool
	NetworkError    bool
	CreateExecError bool
	InspectError    bool
	ListError       bool
	PullError       bool
	RemoveError     bool
	StartError      bool
	StartExecError  bool
}

// NewMock creates a mock docker client suitable for use in unit tests, and a MockClient
// that allows testers to manipulate it's behavior.
func NewMock() (*MockClient, Client) {
	md := &MockClient{
		Mutex:        &sync.Mutex{},
		Pulled:       map[string]struct{}{},
		Containers:   map[string]mockContainer{},
		Networks:     map[string]*dkc.Network{},
		createdExecs: map[string]dkc.CreateExecOptions{},
		Executions:   map[string][]string{},
	}
	return md, Client{md, &sync.Mutex{}, map[string]*cacheEntry{}}
}

// StartContainer starts the given docker container.
func (dk MockClient) StartContainer(id string, hostConfig *dkc.HostConfig) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.StartError {
		return errors.New("start error")
	}

	container := dk.Containers[id]
	container.Running = true
	container.HostConfig = hostConfig
	dk.Containers[id] = container
	return nil
}

// StopContainer stops the given docker container.
func (dk MockClient) StopContainer(id string) {
	dk.Lock()
	defer dk.Unlock()
	container := dk.Containers[id]
	container.Running = false
	dk.Containers[id] = container
}

// RemoveContainer removes the given docker container.
func (dk MockClient) RemoveContainer(opts dkc.RemoveContainerOptions) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.RemoveError {
		return errors.New("remove error")
	}

	delete(dk.Containers, opts.ID)
	return nil
}

// PullImage pulls the requested image.
func (dk MockClient) PullImage(opts dkc.PullImageOptions,
	auth dkc.AuthConfiguration) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.PullError {
		return errors.New("pull error")
	}

	dk.Pulled[opts.Repository+":"+opts.Tag] = struct{}{}
	return nil
}

// ListContainers lists the running containers.
func (dk MockClient) ListContainers(opts dkc.ListContainersOptions) ([]dkc.APIContainers,
	error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.ListError {
		return nil, errors.New("list error")
	}

	var name string
	if opts.Filters != nil {
		names := opts.Filters["name"]
		if len(names) == 1 {
			name = names[0]
		}
	}

	var apics []dkc.APIContainers
	for id, container := range dk.Containers {
		if !container.Running && !opts.All {
			continue
		}

		if name != "" && container.Name != name {
			continue
		}

		apics = append(apics, dkc.APIContainers{ID: id})
	}
	return apics, nil
}

// CreateNetwork creates a network according to opts.
func (dk MockClient) CreateNetwork(opts dkc.CreateNetworkOptions) (*dkc.Network, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.NetworkError {
		return nil, errors.New("create network error")
	}

	network := &dkc.Network{
		Name:   opts.Name,
		Driver: opts.Driver,
		IPAM:   opts.IPAM,
	}
	dk.Networks[opts.Driver] = network
	return network, nil
}

// InspectContainer returns details of the specified container.
func (dk MockClient) InspectContainer(id string) (*dkc.Container, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.InspectError {
		return nil, errors.New("inspect error")
	}

	container, ok := dk.Containers[id]
	if !ok {
		return nil, ErrNoSuchContainer
	}

	return container.Container, nil
}

// CreateContainer creates a container in accordance with the supplied options.
func (dk *MockClient) CreateContainer(opts dkc.CreateContainerOptions) (*dkc.Container,
	error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.CreateError {
		return nil, errors.New("create error")
	}

	image := opts.Config.Image
	if strings.Count(image, ":") == 0 {
		image = image + ":latest"
	}

	if _, ok := dk.Pulled[image]; !ok {
		return nil, errors.New("create a missing image")
	}

	id := uuid.NewV4().String()

	container := &dkc.Container{
		ID:              id,
		Name:            opts.Name,
		Args:            opts.Config.Cmd,
		Config:          opts.Config,
		HostConfig:      opts.HostConfig,
		NetworkSettings: &dkc.NetworkSettings{},
	}
	dk.Containers[id] = mockContainer{container, false}
	return container, nil
}

// CreateExec creates an execution option to be started by StartExec.
func (dk MockClient) CreateExec(opts dkc.CreateExecOptions) (*dkc.Exec, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.CreateExecError {
		return nil, errors.New("create exec error")
	}

	if _, ok := dk.Containers[opts.Container]; !ok {
		return nil, errors.New("unknown container")
	}

	id := uuid.NewV4().String()
	dk.createdExecs[id] = opts
	return &dkc.Exec{ID: id}, nil
}

// StartExec starts the supplied execution object.
func (dk MockClient) StartExec(id string, opts dkc.StartExecOptions) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.StartExecError {
		return errors.New("start exec error")
	}

	exec, _ := dk.createdExecs[id]
	dk.Executions[exec.Container] = append(dk.Executions[exec.Container],
		strings.Join(exec.Cmd, " "))
	return nil
}

// ResetExec clears the list of created and started executions, for use by the unit
// tests.
func (dk *MockClient) ResetExec() {
	dk.Lock()
	defer dk.Unlock()

	dk.createdExecs = map[string]dkc.CreateExecOptions{}
	dk.Executions = map[string][]string{}
}

// UploadToContainer is not implemented.
func (dk MockClient) UploadToContainer(id string,
	opts dkc.UploadToContainerOptions) error {
	panic("MockClient Not Implemented")
}

// DownloadFromContainer is not implemented.
func (dk MockClient) DownloadFromContainer(id string,
	opts dkc.DownloadFromContainerOptions) error {
	panic("MockClient Not Implemented")
}
