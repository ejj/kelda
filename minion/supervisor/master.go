package supervisor

import (
	"fmt"
	"strings"

	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/kelda/kelda/util"

	dkc "github.com/fsouza/go-dockerclient"
)

func runMaster() {
	go runMasterSystem()
}

func runMasterSystem() {
	loopLog := util.NewEventTimer("Supervisor")
	for range conn.Trigger(db.MinionTable, db.EtcdTable).C {
		loopLog.LogStart()
		runMasterOnce()
		loopLog.LogEnd()
	}
}

func runMasterOnce() {
	minion := conn.MinionSelf()

	var etcdRow db.Etcd
	if etcdRows := conn.SelectFromEtcd(nil); len(etcdRows) == 1 {
		etcdRow = etcdRows[0]
	}
	etcdIPs := etcdRow.EtcdIPs

	desiredContainers := []docker.RunOptions{
		{
			Name:        images.Ovsdb,
			Image:       ovsImage,
			Args:        []string{"ovsdb-server"},
			VolumesFrom: []string{"minion"},
		},
		{
			Name:  images.Registry,
			Image: imageMap[images.Registry],
		},
	}

	if minion.PrivateIP != "" && len(etcdIPs) != 0 {
		var etcdServerAddrs []string
		for _, ip := range etcdIPs {
			etcdServerAddrs = append(etcdServerAddrs, fmt.Sprintf("http://%s:2379", ip))
		}
		etcdServersStr := strings.Join(etcdServerAddrs, ",")

		ip := minion.PrivateIP
		desiredContainers = append(desiredContainers, etcdContainer(
			fmt.Sprintf("--name=master-%s", ip),
			fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
			fmt.Sprintf("--advertise-client-urls=http://%s:2379", ip),
			fmt.Sprintf("--listen-peer-urls=http://%s:2380", ip),
			fmt.Sprintf("--initial-advertise-peer-urls=http://%s:2380", ip),
			"--listen-client-urls=http://0.0.0.0:2379",
			"--heartbeat-interval="+etcdHeartbeatInterval,
			"--initial-cluster-state=new",
			"--election-timeout="+etcdElectionTimeout,
		), docker.RunOptions{
			Name:        images.KubeAPIServer,
			Image:       kubeImage,
			NetworkMode: "host",
			Mounts: []dkc.HostMount{
				{
					Source: tlsIO.MinionTLSDir,
					Target: tlsIO.MinionTLSDir,
					Type:   "bind",
				},
			},
			Args: []string{
				"kube-apiserver", "--admission-control=AlwaysAdmit",
				"--advertise-address=" + ip, "--etcd-servers=" + etcdServersStr,
				"--tls-cert-file", tlsIO.SignedCertPath(tlsIO.MinionTLSDir),
				"--tls-private-key-file", tlsIO.SignedKeyPath(tlsIO.MinionTLSDir),
				"--kubelet-client-certificate", tlsIO.SignedCertPath(tlsIO.MinionTLSDir),
				"--kubelet-client-key", tlsIO.SignedKeyPath(tlsIO.MinionTLSDir),
				"--insecure-bind-address", "0.0.0.0", // TODO: Only serve insecurely on localhost.
			},
		}, docker.RunOptions{
			Name:        "kube-controller-manager",
			Image:       kubeImage,
			NetworkMode: "host",
			Mounts: []dkc.HostMount{
				{
					Source: tlsIO.MinionTLSDir,
					Target: tlsIO.MinionTLSDir,
					Type:   "bind",
				},
			},
			Args: []string{
				"kube-controller-manager", "--master", "http://localhost:8080",
			},
		}, docker.RunOptions{
			Name:        "kube-scheduler",
			Image:       kubeImage,
			NetworkMode: "host",
			Args: []string{
				"kube-scheduler", "--master", "http://localhost:8080",
			},
		})
	}

	if etcdRow.Leader {
		/* XXX: If we fail to boot ovn-northd, we should give up
		* our leadership somehow.  This ties into the general
		* problem of monitoring health. */
		desiredContainers = append(desiredContainers, docker.RunOptions{
			Name:        images.Ovnnorthd,
			Image:       ovsImage,
			Args:        []string{"ovn-northd"},
			VolumesFrom: []string{"minion"},
		})
	}
	joinContainers(desiredContainers)
}
