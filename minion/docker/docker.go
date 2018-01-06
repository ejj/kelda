package docker

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/util"

	dkc "github.com/fsouza/go-dockerclient"
	log "github.com/sirupsen/logrus"
)

var pullCacheTimeout = time.Minute
var networkTimeout = time.Minute

// ErrNoSuchContainer is the error returned when an operation is requested on a
// non-existent container.
var ErrNoSuchContainer = errors.New("container does not exist")

// A Container as returned by the docker client API.
type Container struct {
	ID       string
	EID      string
	Name     string
	Image    string
	ImageID  string
	IP       string
	Hostname string
	Mac      string
	Path     string
	Status   string
	Args     []string
	Pid      int
	Env      map[string]string
	Labels   map[string]string
	Created  time.Time
}

// ContainerSlice is an alias for []Container to allow for joins
type ContainerSlice []Container

// A Client to the local docker daemon.
type Client struct {
	client
	*sync.Mutex
	imageCache map[string]*cacheEntry
}

type cacheEntry struct {
	sync.Mutex
	expiration time.Time
}

// RunOptions changes the behavior of the Run function.
type RunOptions struct {
	Name              string
	Image             string
	Args              []string
	Labels            map[string]string
	Env               map[string]string
	FilepathToContent map[string]string

	IP          string
	Hostname    string
	NetworkMode string
	DNS         []string
	DNSSearch   []string

	PidMode     string
	Privileged  bool
	VolumesFrom []string
	CapAdd      []string
	Mounts      []dkc.HostMount
}

type client interface {
	StartContainer(id string, hostConfig *dkc.HostConfig) error
	UploadToContainer(id string, opts dkc.UploadToContainerOptions) error
	RemoveContainer(opts dkc.RemoveContainerOptions) error
	BuildImage(opts dkc.BuildImageOptions) error
	PullImage(opts dkc.PullImageOptions, auth dkc.AuthConfiguration) error
	PushImage(opts dkc.PushImageOptions, auth dkc.AuthConfiguration) error
	ListContainers(opts dkc.ListContainersOptions) ([]dkc.APIContainers, error)
	InspectContainer(id string) (*dkc.Container, error)
	InspectImage(id string) (*dkc.Image, error)
	CreateContainer(dkc.CreateContainerOptions) (*dkc.Container, error)
	CreateNetwork(dkc.CreateNetworkOptions) (*dkc.Network, error)
	ListNetworks() ([]dkc.Network, error)
}

var c = counter.New("Docker")

// New creates client to the docker daemon.
func New(sock string) Client {
	var client *dkc.Client
	for {
		var err error
		client, err = dkc.NewClient(sock)
		if err != nil {
			log.WithError(err).Warn("Failed to create docker client.")
			time.Sleep(10 * time.Second)
			continue
		}
		break
	}

	return Client{client, &sync.Mutex{}, map[string]*cacheEntry{}}
}

// Run creates and starts a new container in accordance RunOptions.
func (dk Client) Run(opts RunOptions) (string, error) {
	c.Inc("Run")

	env := []string{}
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}

	hc := &dkc.HostConfig{
		NetworkMode: opts.NetworkMode,
		PidMode:     opts.PidMode,
		Privileged:  opts.Privileged,
		VolumesFrom: opts.VolumesFrom,
		DNS:         opts.DNS,
		DNSSearch:   opts.DNSSearch,
		CapAdd:      opts.CapAdd,
		Mounts:      opts.Mounts,
	}

	id, err := dk.create(opts.Name, opts.Image, opts.Hostname, opts.Args,
		opts.Labels, env, opts.FilepathToContent, hc, nil)
	if err != nil {
		return "", err
	}

	if err = dk.StartContainer(id, hc); err != nil {
		dk.RemoveID(id) // Remove the container to avoid a zombie.
		return "", err
	}

	return id, nil
}

// ConfigureNetwork makes a request to docker to create a network running on driver.
func (dk Client) ConfigureNetwork(driver string) error {
	c.Inc("Configure Network")

	networks, err := dk.ListNetworks()
	if err == nil {
		for _, nw := range networks {
			if nw.Name == driver {
				return nil
			}
		}
	}

	_, err = dk.CreateNetwork(dkc.CreateNetworkOptions{
		Name:   driver,
		Driver: driver,
		IPAM: dkc.IPAMOptions{
			Config: []dkc.IPAMConfig{{
				Subnet:  ipdef.KeldaSubnet.String(),
				Gateway: ipdef.GatewayIP.String(),
			}},
		},
	})

	return err
}

// Remove stops and deletes the container with the given name.
func (dk Client) Remove(name string) error {
	id, err := dk.getID(name)
	if err != nil {
		return err
	}

	return dk.RemoveID(id)
}

// RemoveID stops and deletes the container with the given ID.
func (dk Client) RemoveID(id string) error {
	c.Inc("Remove")
	err := dk.RemoveContainer(dkc.RemoveContainerOptions{ID: id, Force: true})
	if err != nil {
		return err
	}

	return nil
}

// Build builds an image with the given name and Dockerfile.
func (dk Client) Build(name, dockerfile string, useCache bool) error {
	c.Inc("Build")
	tarBuf, err := util.ToTar("Dockerfile", 0644, dockerfile)
	if err != nil {
		return err
	}

	return dk.BuildImage(dkc.BuildImageOptions{
		NetworkMode:  "host",
		Name:         name,
		InputStream:  tarBuf,
		OutputStream: ioutil.Discard,
		NoCache:      !useCache,
	})
}

// Pull retrieves the given docker image from an image cache.
// The `image` argument can be of the form <repo>, <repo>:<tag>, or
// <repo>:<tag>@<digestFormat>:<digest>.
// If no tag is specified, then the "latest" tag is applied.
func (dk Client) Pull(image string) error {
	c.Inc("Pull")
	repo, tag := dkc.ParseRepositoryTag(image)
	if tag == "" {
		tag = "latest"
	}

	entry := dk.getCacheEntry(repo, tag)
	entry.Lock()
	defer entry.Unlock()

	if time.Now().Before(entry.expiration) {
		return nil
	}

	log.WithField("image", image).Info("Begin image pull")
	opts := dkc.PullImageOptions{Repository: repo,
		Tag:               tag,
		InactivityTimeout: networkTimeout,
	}
	if err := dk.PullImage(opts, dkc.AuthConfiguration{}); err != nil {
		return fmt.Errorf("pull image error: %s", err)
	}

	entry.expiration = time.Now().Add(pullCacheTimeout)
	log.WithField("image", image).Info("Finish image pull")
	return nil
}

func (dk Client) getCacheEntry(repo, tag string) *cacheEntry {
	dk.Lock()
	defer dk.Unlock()

	key := repo + ":" + tag
	if entry, ok := dk.imageCache[key]; ok {
		return entry
	}
	entry := &cacheEntry{}
	dk.imageCache[key] = entry
	return entry
}

// Push pushes the given image to the registry.
func (dk Client) Push(registry, image string) (string, error) {
	c.Inc("Push")
	repo, tag := dkc.ParseRepositoryTag(image)
	err := dk.PushImage(dkc.PushImageOptions{
		Registry: registry,
		Name:     repo,
		Tag:      tag,
	}, dkc.AuthConfiguration{})
	if err != nil {
		return "", err
	}

	img, err := dk.InspectImage(image)
	if err != nil {
		return "", err
	}

	if len(img.RepoDigests) != 1 {
		return "", fmt.Errorf(
			"unexpected number of repo digests (expected exactly one): %v",
			img.RepoDigests)
	}

	return img.RepoDigests[0], nil
}

// List returns a slice of all running containers.  The List can be be filtered with the
// supplied `filters` map.
func (dk Client) List(filters map[string][]string) ([]Container, error) {
	c.Inc("List")
	return dk.list(filters, false)
}

func (dk Client) list(filters map[string][]string, all bool) ([]Container, error) {
	opts := dkc.ListContainersOptions{All: all, Filters: filters}
	apics, err := dk.ListContainers(opts)
	if err != nil {
		return nil, err
	}

	var containers []Container
	for _, apic := range apics {
		c, err := dk.Get(apic.ID)
		if err != nil {
			log.WithError(err).Warnf("Failed to inspect container: %s",
				apic.ID)
			continue
		}

		containers = append(containers, c)
	}

	return containers, nil
}

// Get returns a Container corresponding to the supplied ID.
func (dk Client) Get(id string) (Container, error) {
	c.Inc("Get")
	dkc, err := dk.InspectContainer(id)
	if err != nil {
		return Container{}, err
	}

	env := make(map[string]string)
	for _, value := range dkc.Config.Env {
		e := strings.SplitN(value, "=", 2)
		if len(e) > 1 {
			env[e[0]] = e[1]
		}
	}

	c := Container{
		Name:     dkc.Name,
		ID:       dkc.ID,
		Hostname: dkc.Config.Hostname,
		Image:    dkc.Config.Image,
		ImageID:  dkc.Image,
		Path:     dkc.Path,
		Args:     dkc.Args,
		Pid:      dkc.State.Pid,
		Env:      env,
		Labels:   dkc.Config.Labels,
		Status:   dkc.State.Status,
		Created:  dkc.Created,
	}

	return c, nil
}

func keys(networks map[string]dkc.ContainerNetwork) []string {
	keySet := []string{}
	for key := range networks {
		keySet = append(keySet, key)
	}
	return keySet
}

// IsRunning returns true if the container with the given `name` is running.
func (dk Client) IsRunning(name string) (bool, error) {
	c.Inc("Is Running?")
	containers, err := dk.List(map[string][]string{
		"name": {name},
	})
	if err != nil {
		return false, err
	}
	return len(containers) != 0, nil
}

func (dk Client) create(name, image, hostname string, args []string,
	labels map[string]string, env []string, filepathToContent map[string]string,
	hc *dkc.HostConfig, nc *dkc.NetworkingConfig) (string, error) {

	if err := dk.Pull(image); err != nil {
		return "", err
	}

	container, err := dk.CreateContainer(dkc.CreateContainerOptions{
		Name: name,
		Config: &dkc.Config{
			Image:    string(image),
			Hostname: hostname,
			Cmd:      args,
			Labels:   labels,
			Env:      env},
		HostConfig:       hc,
		NetworkingConfig: nc,
	})
	if err != nil {
		return "", err
	}

	for path, content := range filepathToContent {
		dir := "."
		if filepath.IsAbs(path) {
			dir = "/"
		}

		// We can safely ignore the error returned by `filepath.Rel` because
		// dir can only be `.` or `/`.
		relPath, _ := filepath.Rel(dir, path)
		tarBuf, err := util.ToTar(relPath, 0644, content)
		if err != nil {
			return "", err
		}

		err = dk.UploadToContainer(container.ID, dkc.UploadToContainerOptions{
			InputStream: tarBuf,
			Path:        dir,
		})
		if err != nil {
			return "", err
		}
	}

	return container.ID, nil
}

func (dk Client) getID(name string) (string, error) {
	containers, err := dk.list(map[string][]string{"name": {name}}, true)
	if err != nil {
		return "", err
	}

	if len(containers) > 0 {
		return containers[0].ID, nil
	}

	return "", ErrNoSuchContainer
}

// Get returns the value contained at the given index
func (cs ContainerSlice) Get(ii int) interface{} {
	return cs[ii]
}

// Len returns the number of items in the slice
func (cs ContainerSlice) Len() int {
	return len(cs)
}
