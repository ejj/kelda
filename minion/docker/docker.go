package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
	dkc "github.com/fsouza/go-dockerclient"
)

var pullCacheTimeout = time.Minute

// ErrNoSuchContainer is the error returned when an operation is requested on a
// non-existent container.
var ErrNoSuchContainer = errors.New("container does not exist")

// A Container as returned by the docker client API.
type Container struct {
	ID     string
	EID    string
	Name   string
	Image  string
	IP     string
	Mac    string
	Path   string
	Args   []string
	Pid    int
	Env    map[string]string
	Labels map[string]string
}

// ContainerSlice is an alias for []Container to allow for joins
type ContainerSlice []Container

// A Client to the local docker daemon.
type Client struct {
	client
	*sync.Mutex
	imageCache map[string]time.Time
}

// RunOptions changes the behavior of the Run function.
type RunOptions struct {
	Name   string
	Image  string
	Args   []string
	Labels map[string]string
	Env    map[string]string

	NetworkMode string
	PidMode     string
	Privileged  bool
	VolumesFrom []string
}

type client interface {
	StartContainer(id string, hostConfig *dkc.HostConfig) error
	UploadToContainer(id string, opts dkc.UploadToContainerOptions) error
	DownloadFromContainer(id string, opts dkc.DownloadFromContainerOptions) error
	RemoveContainer(opts dkc.RemoveContainerOptions) error
	PullImage(opts dkc.PullImageOptions, auth dkc.AuthConfiguration) error
	ListContainers(opts dkc.ListContainersOptions) ([]dkc.APIContainers, error)
	InspectContainer(id string) (*dkc.Container, error)
	CreateContainer(dkc.CreateContainerOptions) (*dkc.Container, error)
	CreateNetwork(dkc.CreateNetworkOptions) (*dkc.Network, error)
}

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

	return Client{client, &sync.Mutex{}, map[string]time.Time{}}
}

// Run creates and starts a new container in accordance RunOptions.
func (dk Client) Run(opts RunOptions) (string, error) {
	env := map[string]struct{}{}
	for k, v := range opts.Env {
		env[k+"="+v] = struct{}{}
	}

	hc := &dkc.HostConfig{
		NetworkMode: opts.NetworkMode,
		PidMode:     opts.PidMode,
		Privileged:  opts.Privileged,
		VolumesFrom: opts.VolumesFrom,
		DNSSearch:   []string{"q"},
	}

	id, err := dk.create(opts.Name, opts.Image, opts.Args, opts.Labels, env, hc, nil)
	if err != nil {
		return "", err
	}

	if err = dk.StartContainer(id, hc); err != nil {
		dk.RemoveID(id) // Remove the container to avoid a zombie.
		return "", err
	}

	return id, nil
}

// ConfigureNetwork makes a request to docker to create a network running on driver with
// the given subnet.
func (dk Client) ConfigureNetwork(driver string, subnet net.IPNet) error {
	_, err := dk.CreateNetwork(dkc.CreateNetworkOptions{
		Name:   driver,
		Driver: driver,
		IPAM: dkc.IPAMOptions{
			Config: []dkc.IPAMConfig{{
				Subnet:  ipdef.QuiltSubnet.String(),
				IPRange: subnet.String(),
				Gateway: ipdef.GatewayIP.String(),
			}},
		},
	})
	return err
}

// WriteToContainer writes the contents of SRC into the file at path DST on the
// container with id ID. Overwrites DST if it already exists.
func (dk Client) WriteToContainer(id, src, dst, archiveName string,
	permission int) error {

	tarBuf, err := util.ToTar(archiveName, permission, src)

	if err != nil {
		return err
	}

	err = dk.UploadToContainer(id, dkc.UploadToContainerOptions{
		InputStream: tarBuf,
		Path:        dst,
	})
	if err != nil {
		return err
	}

	return nil
}

// GetFromContainer returns a string containing the content of the file named
// SRC on the container with id ID.
func (dk Client) GetFromContainer(id string, src string) (string, error) {
	var buffIn bytes.Buffer
	var buffOut bytes.Buffer
	err := dk.DownloadFromContainer(id, dkc.DownloadFromContainerOptions{
		OutputStream: &buffIn,
		Path:         src,
	})
	if err != nil {
		return "", err
	}

	writer := io.Writer(&buffOut)

	for tr := tar.NewReader(&buffIn); err != io.EOF; _, err = tr.Next() {

		if err != nil {
			return "", err
		}

		_, err = io.Copy(writer, tr)
		if err != nil {
			return "", err
		}
	}

	return buffOut.String(), nil
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
	err := dk.RemoveContainer(dkc.RemoveContainerOptions{ID: id, Force: true})
	if err != nil {
		return err
	}

	return nil
}

// Pull retrieves the given docker image from an image cache.
// The `image` argument can be of the form <repo>, <repo>:<tag>, or
// <repo>:<tag>@<digestFormat>:<digest>.
// If no tag is specified, then the "latest" tag is applied.
func (dk Client) Pull(image string) error {
	dk.Lock()
	defer dk.Unlock()

	now := time.Now()
	if expiration, ok := dk.imageCache[image]; ok && now.Before(expiration) {
		return nil
	}

	repo, tag := dkc.ParseRepositoryTag(image)
	if tag == "" {
		tag = "latest"
	}

	log.WithField("image", image).Info("Begin image pull")
	opts := dkc.PullImageOptions{Repository: repo, Tag: tag}
	if err := dk.PullImage(opts, dkc.AuthConfiguration{}); err != nil {
		log.WithField("image", image).WithError(err).Error("Failed image pull")
		return err
	}

	dk.imageCache[repo+":"+tag] = now.Add(pullCacheTimeout)
	log.WithField("image", image).Info("Finish image pull")
	return nil
}

// List returns a slice of all running containers.  The List can be be filtered with the
// supplied `filters` map.
func (dk Client) List(filters map[string][]string) ([]Container, error) {
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
	dkc, err := dk.InspectContainer(id)
	if err != nil {
		return Container{}, err
	}

	env := make(map[string]string)
	for _, value := range dkc.Config.Env {
		e := strings.Split(value, "=")
		if len(e) > 1 {
			env[e[0]] = e[1]
		}
	}

	c := Container{
		Name:   dkc.Name,
		ID:     dkc.ID,
		IP:     dkc.NetworkSettings.IPAddress,
		Mac:    dkc.NetworkSettings.MacAddress,
		EID:    dkc.NetworkSettings.EndpointID,
		Image:  dkc.Config.Image,
		Path:   dkc.Path,
		Args:   dkc.Args,
		Pid:    dkc.State.Pid,
		Env:    env,
		Labels: dkc.Config.Labels,
	}

	networks := keys(dkc.NetworkSettings.Networks)
	if len(networks) == 1 {
		config := dkc.NetworkSettings.Networks[networks[0]]
		c.IP = config.IPAddress
		c.Mac = config.MacAddress
		c.EID = config.EndpointID
	} else if len(networks) > 1 {
		log.Warnf("Multiple networks for container: %s", dkc.ID)
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
	containers, err := dk.List(map[string][]string{
		"name": {name},
	})
	if err != nil {
		return false, err
	}
	return len(containers) != 0, nil
}

func (dk Client) create(name, image string, args []string, labels map[string]string,
	env map[string]struct{}, hc *dkc.HostConfig, nc *dkc.NetworkingConfig) (string,
	error) {

	if err := dk.Pull(image); err != nil {
		return "", err
	}

	envList := make([]string, len(env))
	for k := range env {
		envList = append(envList, k)
	}

	container, err := dk.CreateContainer(dkc.CreateContainerOptions{
		Name: name,
		Config: &dkc.Config{
			Image:  string(image),
			Cmd:    args,
			Labels: labels,
			Env:    envList},
		HostConfig:       hc,
		NetworkingConfig: nc,
	})
	if err != nil {
		return "", err
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
