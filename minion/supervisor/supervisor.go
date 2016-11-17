package supervisor

import (
	"fmt"
	"os/exec"
	"reflect"
	"strings"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/docker"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
)

const (
	// Etcd is the name etcd cluster store container.
	Etcd = "etcd"

	// Ovncontroller is the name of the OVN controller container.
	Ovncontroller = "ovn-controller"

	// Ovnnorthd is the name of the OVN northd container.
	Ovnnorthd = "ovn-northd"

	// Ovsdb is the name of the OVSDB container.
	Ovsdb = "ovsdb-server"

	// Ovsvswitchd is the name of the ovs-vswitchd container.
	Ovsvswitchd = "ovs-vswitchd"
)

const ovsImage = "quilt/ovs"

var images = map[string]string{
	Etcd:          "quay.io/coreos/etcd:v3.0.2",
	Ovncontroller: ovsImage,
	Ovnnorthd:     ovsImage,
	Ovsdb:         ovsImage,
	Ovsvswitchd:   ovsImage,
}

const etcdHeartbeatInterval = "500"
const etcdElectionTimeout = "5000"

type supervisor struct {
	conn db.Conn
	dk   docker.Client

	role     db.Role
	etcdIPs  []string
	leaderIP string
	IP       string
	leader   bool
	provider string
	region   string
	size     string
}

// Run blocks implementing the supervisor module.
func Run(conn db.Conn, dk docker.Client) {
	sv := supervisor{conn: conn, dk: dk}
	sv.runSystem()
}

// Manage system infrstracture containers that support the application.
func (sv *supervisor) runSystem() {
	imageSet := map[string]struct{}{}
	for _, image := range images {
		imageSet[image] = struct{}{}
	}

	for image := range imageSet {
		go sv.dk.Pull(image)
	}

	loopLog := util.NewEventTimer("Supervisor")
	for range sv.conn.Trigger(db.MinionTable, db.EtcdTable).C {
		loopLog.LogStart()
		sv.runSystemOnce()
		loopLog.LogEnd()
	}
}

func (sv *supervisor) runSystemOnce() {
	minion, err := sv.conn.MinionSelf()
	if err != nil {
		return
	}

	var etcdRow db.Etcd
	if etcdRows := sv.conn.SelectFromEtcd(nil); len(etcdRows) == 1 {
		etcdRow = etcdRows[0]
	}

	if sv.role == minion.Role &&
		reflect.DeepEqual(sv.etcdIPs, etcdRow.EtcdIPs) &&
		sv.leaderIP == etcdRow.LeaderIP &&
		sv.IP == minion.PrivateIP &&
		sv.leader == etcdRow.Leader &&
		sv.provider == minion.Provider &&
		sv.region == minion.Region &&
		sv.size == minion.Size {
		return
	}

	if minion.Role != sv.role {
		sv.RemoveAll()
	}

	switch minion.Role {
	case db.Master:
		sv.updateMaster(minion.PrivateIP, etcdRow.EtcdIPs,
			etcdRow.Leader)
	case db.Worker:
		sv.updateWorker(minion.PrivateIP, etcdRow.LeaderIP,
			etcdRow.EtcdIPs)
	}

	sv.role = minion.Role
	sv.etcdIPs = etcdRow.EtcdIPs
	sv.leaderIP = etcdRow.LeaderIP
	sv.IP = minion.PrivateIP
	sv.leader = etcdRow.Leader
	sv.provider = minion.Provider
	sv.region = minion.Region
	sv.size = minion.Size
}

func (sv *supervisor) updateWorker(IP string, leaderIP string, etcdIPs []string) {
	if !reflect.DeepEqual(sv.etcdIPs, etcdIPs) {
		sv.Remove(Etcd)
	}

	sv.run(Etcd, fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		"--heartbeat-interval="+etcdHeartbeatInterval,
		"--election-timeout="+etcdElectionTimeout,
		"--proxy=on")

	sv.run(Ovsdb, "ovsdb-server")
	sv.run(Ovsvswitchd, "ovs-vswitchd")

	if leaderIP == "" || IP == "" {
		return
	}

	err := execRun("ovs-vsctl", "set", "Open_vSwitch", ".",
		fmt.Sprintf("external_ids:ovn-remote=\"tcp:%s:6640\"", leaderIP),
		fmt.Sprintf("external_ids:ovn-encap-ip=%s", IP),
		"external_ids:ovn-encap-type=\"geneve\"",
		fmt.Sprintf("external_ids:api_server=\"http://%s:9000\"", leaderIP),
		fmt.Sprintf("external_ids:system-id=\"%s\"", IP),
		"--", "add-br", "quilt-int",
		"--", "set", "bridge", "quilt-int", "fail_mode=secure")
	if err != nil {
		log.WithError(err).Warnf("Failed to exec in %s.", Ovsvswitchd)
	}

	/* The ovn controller doesn't support reconfiguring ovn-remote mid-run.
	 * So, we need to restart the container when the leader changes. */
	sv.Remove(Ovncontroller)
	sv.run(Ovncontroller, "ovn-controller")
}

func (sv *supervisor) updateMaster(IP string, etcdIPs []string, leader bool) {
	if sv.IP != IP || !reflect.DeepEqual(sv.etcdIPs, etcdIPs) {
		sv.Remove(Etcd)
	}

	if IP == "" || len(etcdIPs) == 0 {
		return
	}

	sv.run(Etcd, fmt.Sprintf("--name=master-%s", IP),
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		fmt.Sprintf("--advertise-client-urls=http://%s:2379", IP),
		fmt.Sprintf("--listen-peer-urls=http://%s:2380", IP),
		fmt.Sprintf("--initial-advertise-peer-urls=http://%s:2380", IP),
		"--listen-client-urls=http://0.0.0.0:2379",
		"--heartbeat-interval="+etcdHeartbeatInterval,
		"--initial-cluster-state=new",
		"--election-timeout="+etcdElectionTimeout)
	sv.run(Ovsdb, "ovsdb-server")

	if leader {
		/* XXX: If we fail to boot ovn-northd, we should give up
		* our leadership somehow.  This ties into the general
		* problem of monitoring health. */
		sv.run(Ovnnorthd, "ovn-northd")
	} else {
		sv.Remove(Ovnnorthd)
	}
}

func (sv *supervisor) run(name string, args ...string) {
	isRunning, err := sv.dk.IsRunning(name)
	if err != nil {
		log.WithError(err).Warnf("could not check running status of %s.", name)
		return
	}
	if isRunning {
		return
	}

	ro := docker.RunOptions{
		Name:        name,
		Image:       images[name],
		Args:        args,
		NetworkMode: "host",
		VolumesFrom: []string{"minion"},
	}

	if name == Ovsvswitchd {
		ro.Privileged = true
	}

	log.Infof("Start Container: %s", name)
	_, err = sv.dk.Run(ro)
	if err != nil {
		log.WithError(err).Warnf("Failed to run %s.", name)
	}
}

func (sv *supervisor) Remove(name string) {
	log.WithField("name", name).Info("Removing container")
	err := sv.dk.Remove(name)
	if err != nil && err != docker.ErrNoSuchContainer {
		log.WithError(err).Warnf("Failed to remove %s.", name)
	}
}

func (sv *supervisor) RemoveAll() {
	for name := range images {
		sv.Remove(name)
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
	return exec.Command(name, arg...).Run()
}
