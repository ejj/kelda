package supervisor

import (
	"fmt"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/supervisor/images"
	"strings"

	dkc "github.com/fsouza/go-dockerclient"
)

const etcdDataDir = "/etcd-data"
const etcdHeartbeatInterval = "500"
const etcdElectionTimeout = "5000"

func etcdContainer(args ...string) docker.RunOptions {
	return docker.RunOptions{
		Name:  images.Etcd,
		Image: imageMap[images.Etcd],
		// Run etcd with a data directory that's mounted on the host disk.
		// This way, if the container restarts, its previous state will still be
		// available.
		Mounts: []dkc.HostMount{
			{
				Target: etcdDataDir,
				Source: "/var/lib/etcd",
				Type:   "bind",
			},
		},
		Env: map[string]string{
			"ETCD_DATA_DIR": etcdDataDir,
		},
		Args: append([]string{"etcd"}, args...),
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
