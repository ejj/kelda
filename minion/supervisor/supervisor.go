package supervisor

import (
	"os/exec"
	"strings"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/kelda/kelda/util/str"
	"github.com/kelda/kelda/version"

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
	images.KubeAPIServer: version.Image,
	// TODO: Add other Kube images, and think through whether this map is
	// actually useful.
}

var c = counter.New("Supervisor")

var conn db.Conn
var dk docker.Client

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

const containerTypeKey = "containerType"
const sysContainerVal = "keldaSystemContainer"

// joinContainers boots and stops system containers so that only the
// desiredContainers are running. Note that only containers with the
// keldaSystemContainer tag are considered. Other containers, such as blueprint
// containers, or containers manually created on the host, are ignored.
func joinContainers(desiredContainers []docker.RunOptions) {
	actual, err := dk.List(map[string][]string{
		"label": {containerTypeKey + "=" + sysContainerVal}})
	if err != nil {
		log.WithError(err).Error("Failed to list current containers")
		return
	}

	_, toBoot, toStop := join.Join(desiredContainers, actual, syncContainersScore)

	for _, intf := range toStop {
		// Docker prepends a leading "/" to container names.
		name := strings.TrimPrefix(intf.(docker.Container).Name, "/")
		log.WithField("name", name).Info("Stopping system container")
		c.Inc("Docker Remove " + name)
		if err := dk.Remove(name); err != nil {
			log.WithError(err).WithField("name", name).
				Error("Failed to remove container")
		}
	}

	for _, intf := range toBoot {
		ro := intf.(docker.RunOptions)
		log.WithField("name", ro.Name).Info("Booting system container")
		c.Inc("Docker Run " + ro.Name)

		if ro.Labels == nil {
			ro.Labels = map[string]string{}
		}
		ro.Labels[containerTypeKey] = sysContainerVal
		ro.NetworkMode = "host"

		if _, err := dk.Run(ro); err != nil {
			log.WithError(err).WithField("name", ro.Name).
				Error("Failed to run container")
		}
	}
}

// For simplicity, syncContainersScore only considers the container attributes
// that might change. For example, VolumesFrom, NetworkMode, and
// FilepathToContent aren't considered.
func syncContainersScore(left, right interface{}) int {
	ro := left.(docker.RunOptions)
	dkc := right.(docker.Container)

	if ro.Image != dkc.Image {
		return -1
	}

	for key, value := range ro.Env {
		if dkc.Env[key] != value {
			return -1
		}
	}

	// Depending on the container, the command in the database could be
	// either the command plus it's arguments, or just it's arguments.  To
	// handle that case, we check both.
	cmd1 := dkc.Args
	cmd2 := append([]string{dkc.Path}, dkc.Args...)
	if len(ro.Args) != 0 &&
		!str.SliceEq(ro.Args, cmd1) &&
		!str.SliceEq(ro.Args, cmd2) {
		return -1
	}

	return 0
}

// execRun() is a global variable so that it can be mocked out by the unit tests.
var execRun = func(name string, arg ...string) ([]byte, error) {
	c.Inc(name)
	return exec.Command(name, arg...).Output()
}
