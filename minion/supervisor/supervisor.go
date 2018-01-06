package supervisor

import (
	"crypto/sha1"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/util/str"
	"github.com/kelda/kelda/version"

	log "github.com/sirupsen/logrus"
)

// Friendly names for containers. These are identifiers that could be used with
// `docker logs`.
const (
	// EtcdName is the name etcd cluster store container.
	EtcdName = "etcd"

	// OvncontrollerName is the name of the OVN controller container.
	OvncontrollerName = "ovn-controller"

	// OvnnorthdName is the name of the OVN northd container.
	OvnnorthdName = "ovn-northd"

	// OvsdbName is the name of the OVSDB container.
	OvsdbName = "ovsdb-server"

	// OvsvswitchdName is the name of the ovs-vswitchd container.
	OvsvswitchdName = "ovs-vswitchd"

	// RegistryName is the name of the registry container.
	RegistryName = "registry"

	// KubeAPIServerName is the name of the Kubernetes API server container.
	KubeAPIServerName = "kube-apiserver"

	// KubeletName is the name of the Kubernetes kubelet container.
	KubeletName = "kubelet"

	// KubeControllerManagerName is the name of the Kubernetes controller
	// manager container.
	KubeControllerManagerName = "kube-controller-manager"

	// KubeSchedulerName is the name of the Kubernetes scheduler container.
	KubeSchedulerName = "kube-scheduler"
)

// The names of the images to be run. These are identifier that could be used
// with `docker run`.
const (
	ovsImage      = "keldaio/ovs"
	etcdImage     = "quay.io/coreos/etcd:v3.3"
	registryImage = "registry:2.6.2"
	kubeImage     = version.Image
)

const (
	containerTypeKey = "containerType"
	sysContainerVal  = "keldaSystemContainer"
	filesKey         = "files"
)

// The tunneling protocol to use between machines.
// "stt" and "geneve" are supported.
const tunnelingProtocol = "stt"

var c = counter.New("Supervisor")

var conn db.Conn
var dk docker.Client

// Run blocks implementing the supervisor module.
func Run(_conn db.Conn, _dk docker.Client, role db.Role) {
	conn = _conn
	dk = _dk

	images := []string{ovsImage, etcdImage, kubeImage}
	if role == db.Master {
		images = append(images, registryImage)
	}

	for _, image := range images {
		go dk.Pull(image)
	}

	switch role {
	case db.Master:
		runMaster()
	case db.Worker:
		runWorker()
	}
}

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
		dkc := intf.(docker.Container)
		// Docker prepends a leading "/" to container names.
		name := strings.TrimPrefix(dkc.Name, "/")
		log.WithField("name", name).Info("Stopping system container")
		c.Inc("Docker Remove " + name)
		if err := dk.RemoveID(dkc.ID); err != nil {
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
		ro.Labels[filesKey] = filesHash(ro.FilepathToContent)
		ro.NetworkMode = "host"

		if _, err := dk.Run(ro); err != nil {
			log.WithError(err).WithField("name", ro.Name).
				Error("Failed to run container")
		}
	}
}

// For simplicity, syncContainersScore only considers the container attributes
// that might change. For example, VolumesFrom and NetworkMode aren't
// considered.
func syncContainersScore(left, right interface{}) int {
	ro := left.(docker.RunOptions)
	dkc := right.(docker.Container)

	expFilesHash := filesHash(ro.FilepathToContent)
	if ro.Image != dkc.Image || dkc.Labels[filesKey] != expFilesHash {
		return -1
	}

	for key, value := range ro.Env {
		if dkc.Env[key] != value {
			return -1
		}
	}

	// Container.Args isn't necessarily the same as RunOptions.Args even if the
	// Container was booted with the given RunOptions. This is because of the
	// way Docker sets the Path field -- if a container doesn't have an
	// Entrypoint, then the Path field gets set to the first argument in
	// RunOptions.Args, and that first argument is removed from Container.Args.
	// If the image does have an Entrypoint set, then the Path will be the
	// Entrypoint, and Container.Args and RunOptions.Args are equivalent. To
	// handle both cases, we check both possible formattings of the Args.
	cmd1 := dkc.Args
	cmd2 := append([]string{dkc.Path}, dkc.Args...)
	if len(ro.Args) != 0 &&
		!str.SliceEq(ro.Args, cmd1) &&
		!str.SliceEq(ro.Args, cmd2) {
		return -1
	}

	return 0
}

func filesHash(filepathToContent map[string]string) string {
	toHash := str.MapAsString(filepathToContent)
	return fmt.Sprintf("%x", sha1.Sum([]byte(toHash)))
}

// execRun() is a global variable so that it can be mocked out by the unit tests.
var execRun = func(name string, arg ...string) ([]byte, error) {
	c.Inc(name)
	return exec.Command(name, arg...).Output()
}
