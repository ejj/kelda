package supervisor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/supervisor/images"

	dkc "github.com/fsouza/go-dockerclient"
	log "github.com/sirupsen/logrus"
)

const ovsImage = "keldaio/ovs"

// The tunneling protocol to use between machines.
// "stt" and "geneve" are supported.
const tunnelingProtocol = "stt"

var imageMap = map[string]string{
	images.Etcd:          "quay.io/coreos/etcd:v3.3",
	images.Ovncontroller: ovsImage,
	images.Ovnnorthd:     ovsImage,
	images.Ovsdb:         ovsImage,
	images.Ovsvswitchd:   ovsImage,
	images.Registry:      "registry:2.6.2",
}

const etcdHeartbeatInterval = "500"
const etcdElectionTimeout = "5000"

var c = counter.New("Supervisor")

var conn db.Conn
var dk docker.Client
var oldEtcdIPs []string
var oldIP string

// Run blocks implementing the supervisor module.
func Run(_conn db.Conn, _dk docker.Client, role db.Role) {
	conn = _conn
	dk = _dk

	imageSet := map[string]struct{}{}
	for _, image := range imageMap {
		imageSet[image] = struct{}{}
	}

	for image := range imageSet {
		go dk.Pull(image)
	}

	switch role {
	case db.Master:
		runMaster()
	case db.Worker:
		runWorker()
	}
}

// run calls out to the Docker client to run the container specified by name.
func run(name string, args ...string) {
	c.Inc("Docker Run " + name)
	isRunning, err := dk.IsRunning(name)
	if err != nil {
		log.WithError(err).Warnf("could not check running status of %s.", name)
		return
	}
	if isRunning {
		return
	}

	ro := docker.RunOptions{
		Name:        name,
		Image:       imageMap[name],
		Args:        args,
		NetworkMode: "host",
		VolumesFrom: []string{"minion"},
		Env:         map[string]string{},
	}

	if name == images.Ovsvswitchd {
		ro.Privileged = true
	}

	// Run etcd with a data directory that's mounted on the host disk.
	// This way, if the container restarts, its previous state will still be
	// available.
	if name == images.Etcd {
		etcdDataDir := "/etcd-data"
		ro.Mounts = []dkc.HostMount{
			{
				Target: etcdDataDir,
				Source: "/var/lib/etcd",
				Type:   "bind",
			},
		}
		ro.Env["ETCD_DATA_DIR"] = etcdDataDir
	}

	log.Infof("Start Container: %s", name)
	_, err = dk.Run(ro)
	if err != nil {
		log.WithError(err).Warnf("Failed to run %s.", name)
	}
}

// Remove removes the docker container specified by name.
func Remove(name string) {
	log.WithField("name", name).Info("Removing container")
	err := dk.Remove(name)
	if err != nil && err != docker.ErrNoSuchContainer {
		log.WithError(err).Warnf("Failed to remove %s.", name)
	}
}

func initialClusterString(etcdIPs []string) string {
	var initialCluster []string
	for _, ip := range etcdIPs {
		initialCluster = append(initialCluster,
			fmt.Sprintf("%s=http://%s:2380", nodeName(ip), ip))
	}
	return strings.Join(initialCluster, ",")
}

func nodeName(IP string) string {
	return fmt.Sprintf("master-%s", IP)
}

// execRun() is a global variable so that it can be mocked out by the unit tests.
var execRun = func(name string, arg ...string) error {
	c.Inc(name)
	return exec.Command(name, arg...).Run()
}
