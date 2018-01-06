package supervisor

import (
	"fmt"
	"strings"

	cliPath "github.com/kelda/kelda/cli/path"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/util"

	dkc "github.com/fsouza/go-dockerclient"
)

const encryptionConfigPath = "/var/lib/kubernetes/encryption-config.yaml"

func runMaster() {
	go runMasterSystem()
}

func runMasterSystem() {
	loopLog := util.NewEventTimer("Supervisor")
	for range conn.TriggerTick(30, db.MinionTable, db.EtcdTable).C {
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
			Name:        OvsdbName,
			Image:       ovsImage,
			Args:        []string{"ovsdb-server"},
			VolumesFrom: []string{"minion"},
		},
		{
			Name:  RegistryName,
			Image: registryImage,
		},
	}

	if minion.PrivateIP != "" && len(etcdIPs) != 0 {
		ip := minion.PrivateIP
		desiredContainers = append(desiredContainers, etcdContainer(
			fmt.Sprintf("--name=master-%s", ip),
			"--initial-cluster="+initialClusterString(etcdIPs),
			fmt.Sprintf("--advertise-client-urls=http://%s:2379", ip),
			fmt.Sprintf("--listen-peer-urls=http://%s:2380", ip),
			fmt.Sprintf("--initial-advertise-peer-urls=http://%s:2380", ip),
			"--listen-client-urls=http://0.0.0.0:2379",
			"--heartbeat-interval="+etcdHeartbeatInterval,
			"--initial-cluster-state=new",
			"--election-timeout="+etcdElectionTimeout,
		))

		kubeSecret, err := util.ReadFile(cliPath.MinionKubeSecretPath)
		if err == nil {
			encryptionConfig := fmt.Sprintf(`kind: EncryptionConfig
apiVersion: v1
resources:
  - resources:
      - secrets
    providers:
      - aescbc:
          keys:
            - name: kelda-key
              secret: %s
`, kubeSecret)
			desiredContainers = append(desiredContainers, docker.RunOptions{
				Name:  KubeAPIServerName,
				Image: kubeImage,
				Mounts: []dkc.HostMount{
					{
						Source: cliPath.MinionTLSDir,
						Target: cliPath.MinionTLSDir,
						Type:   "bind",
					},
				},
				Args: kubeAPIServerArgs(ip, etcdIPs),
				FilepathToContent: map[string]string{
					encryptionConfigPath: encryptionConfig,
				},
			}, docker.RunOptions{
				Name:  KubeControllerManagerName,
				Image: kubeImage,
				Mounts: []dkc.HostMount{
					{
						Source: cliPath.MinionTLSDir,
						Target: cliPath.MinionTLSDir,
						Type:   "bind",
					},
				},
				Args: kubeControllerManagerArgs(),
			}, docker.RunOptions{
				Name:  KubeSchedulerName,
				Image: kubeImage,
				Args:  kubeSchedulerArgs(),
			})
		}
	}

	if etcdRow.Leader {
		/* XXX: If we fail to boot ovn-northd, we should give up
		* our leadership somehow.  This ties into the general
		* problem of monitoring health. */
		desiredContainers = append(desiredContainers, docker.RunOptions{
			Name:        OvnnorthdName,
			Image:       ovsImage,
			Args:        []string{"ovn-northd"},
			VolumesFrom: []string{"minion"},
		})
	}
	joinContainers(desiredContainers)
}

func kubeAPIServerArgs(ip string, etcdIPs []string) []string {
	var etcdServerAddrs []string
	for _, ip := range etcdIPs {
		etcdServerAddrs = append(etcdServerAddrs,
			fmt.Sprintf("http://%s:2379", ip))
	}
	etcdServersStr := strings.Join(etcdServerAddrs, ",")

	return []string{
		"kube-apiserver",
		"--admission-control=Initializers,NamespaceLifecycle," +
			"NodeRestriction,LimitRanger,ServiceAccount," +
			"DefaultStorageClass,ResourceQuota",
		"--advertise-address=" + ip,
		"--authorization-mode=Node",
		"--client-ca-file=" + tlsIO.CACertPath(cliPath.MinionTLSDir),
		"--etcd-servers=" + etcdServersStr,
		"--kubelet-certificate-authority=" +
			tlsIO.CACertPath(cliPath.MinionTLSDir),
		"--kubelet-client-certificate=" +
			tlsIO.SignedCertPath(cliPath.MinionTLSDir),
		"--kubelet-client-key=" + tlsIO.SignedKeyPath(cliPath.MinionTLSDir),
		"--tls-ca-file=" + tlsIO.CACertPath(cliPath.MinionTLSDir),
		"--tls-cert-file=" + tlsIO.SignedCertPath(cliPath.MinionTLSDir),
		"--tls-private-key-file=" + tlsIO.SignedKeyPath(cliPath.MinionTLSDir),
		"--anonymous-auth=false",
		"--service-account-key-file=" + tlsIO.SignedKeyPath(cliPath.MinionTLSDir),
		"--experimental-encryption-provider-config=" + encryptionConfigPath,
	}
}

func kubeControllerManagerArgs() []string {
	return []string{
		"kube-controller-manager", "--master=http://localhost:8080",
		"--service-account-private-key-file=" +
			tlsIO.SignedKeyPath(cliPath.MinionTLSDir),
		"--pod-eviction-timeout=30s",
	}
}

func kubeSchedulerArgs() []string {
	return []string{"kube-scheduler", "--master", "http://localhost:8080"}
}
