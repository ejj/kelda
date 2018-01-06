package docker

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"sync"

	dkc "github.com/fsouza/go-dockerclient"
	"github.com/satori/go.uuid"
)

type mockContainer struct {
	*dkc.Container
	Running bool
}

// BuildImageOptions represents the parameters in a call to BuildImage.
type BuildImageOptions struct {
	Name, Dockerfile string
	NoCache          bool
}

// UploadToContainerOptions represents the parameters in a call to UploadToContainer.
type UploadToContainerOptions struct {
	ContainerID, UploadPath, TarPath, Contents string
}

// MockClient gives unit testers access to the internals of the mock docker client
// returned by NewMock.
type MockClient struct {
	*sync.Mutex
	Built      map[BuildImageOptions]struct{}
	Pulled     map[string]struct{}
	Pushed     map[dkc.PushImageOptions]struct{}
	Containers map[string]mockContainer
	Networks   map[string]*dkc.Network
	Uploads    map[UploadToContainerOptions]struct{}
	Images     map[string]*dkc.Image

	createdExecs map[string]dkc.CreateExecOptions
	Executions   map[string][]string

	CreateError           bool
	CreateNetworkError    bool
	ListNetworksError     bool
	CreateExecError       bool
	InspectContainerError bool
	InspectImageError     bool
	ListError             bool
	BuildError            bool
	PullError             bool
	PushError             bool
	RemoveError           bool
	StartError            bool
	StartExecError        bool
	UploadError           bool
}

// NewMock creates a mock docker client suitable for use in unit tests, and a MockClient
// that allows testers to manipulate it's behavior.
func NewMock() (*MockClient, Client) {
	md := &MockClient{
		Mutex:        &sync.Mutex{},
		Built:        map[BuildImageOptions]struct{}{},
		Pulled:       map[string]struct{}{},
		Pushed:       map[dkc.PushImageOptions]struct{}{},
		Containers:   map[string]mockContainer{},
		Networks:     map[string]*dkc.Network{},
		Uploads:      map[UploadToContainerOptions]struct{}{},
		Images:       map[string]*dkc.Image{},
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

func readDockerfile(inp io.Reader) ([]byte, error) {
	tarball := tar.NewReader(inp)
	for {
		hdr, err := tarball.Next()
		if err != nil {
			return nil, fmt.Errorf("malformed build tarball: %s", err.Error())
		}
		if hdr.Name == "Dockerfile" {
			return ioutil.ReadAll(tarball)
		}
	}
}

// BuildImage builds the requested image.
func (dk MockClient) BuildImage(opts dkc.BuildImageOptions) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.BuildError {
		return errors.New("build error")
	}

	dockerfile, err := readDockerfile(opts.InputStream)
	dk.Built[BuildImageOptions{
		Name:       opts.Name,
		Dockerfile: string(dockerfile),
		NoCache:    opts.NoCache,
	}] = struct{}{}
	dk.Images[opts.Name] = &dkc.Image{ID: uuid.NewV4().String()}
	return err
}

// ResetBuilt clears the list of built images, for use by the unit tests.
func (dk *MockClient) ResetBuilt() {
	dk.Lock()
	defer dk.Unlock()
	dk.Built = map[BuildImageOptions]struct{}{}
}

// InspectImage inspects the requested image.
func (dk MockClient) InspectImage(name string) (*dkc.Image, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.InspectImageError {
		return nil, errors.New("inspect image error")
	}

	img, ok := dk.Images[name]
	if !ok {
		return nil, fmt.Errorf("no image with name %s", name)
	}

	return img, nil
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

// PushImage pushes the requested image.
func (dk MockClient) PushImage(opts dkc.PushImageOptions, _ dkc.AuthConfiguration) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.PushError {
		return errors.New("push error")
	}

	imgWithTag := opts.Name
	if opts.Tag != "" {
		imgWithTag += ":" + opts.Tag
	}

	img, ok := dk.Images[imgWithTag]
	if !ok {
		return errors.New("image does not exist")
	}

	img.RepoDigests = []string{"repo-digest-" + img.ID}
	dk.Pushed[opts] = struct{}{}
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

	if dk.CreateNetworkError {
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

// ListNetworks lists all networks.
func (dk MockClient) ListNetworks() ([]dkc.Network, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.ListNetworksError {
		return nil, errors.New("list networks error")
	}

	var networks []dkc.Network
	for _, nw := range dk.Networks {
		networks = append(networks, *nw)
	}
	return networks, nil
}

// InspectContainer returns details of the specified container.
func (dk MockClient) InspectContainer(id string) (*dkc.Container, error) {
	dk.Lock()
	defer dk.Unlock()

	if dk.InspectContainerError {
		return nil, errors.New("inspect error")
	}

	container, ok := dk.Containers[id]
	if !ok {
		return nil, ErrNoSuchContainer
	}

	toReturn := container.Container
	if container.Running {
		toReturn.State = dkc.State{Status: "Running"}
	}

	return toReturn, nil
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
		ID:         id,
		Name:       opts.Name,
		Args:       opts.Config.Cmd,
		Config:     opts.Config,
		HostConfig: opts.HostConfig,
	}
	if img, ok := dk.Images[image]; ok {
		container.Image = img.ID
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

// UploadToContainer extracts a tarball into the given container.
func (dk MockClient) UploadToContainer(id string,
	opts dkc.UploadToContainerOptions) error {
	dk.Lock()
	defer dk.Unlock()

	if dk.UploadError {
		return errors.New("upload error")
	}

	tr := tar.NewReader(opts.InputStream)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		file, err := ioutil.ReadAll(tr)
		if err != nil {
			return err
		}

		dk.Uploads[UploadToContainerOptions{
			ContainerID: id,
			UploadPath:  opts.Path,
			TarPath:     hdr.Name,
			Contents:    string(file),
		}] = struct{}{}
	}

	return nil
}
