package supervisor

import (
	"fmt"
	"net"
	"time"

	cliPath "github.com/kelda/kelda/cli/path"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/docker"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/kubernetes"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/util"

	dkc "github.com/fsouza/go-dockerclient"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
)

func runWorker() {
	setupWorker()
	go runWorkerSystem()
}

func setupWorker() {
	// Boot ovsdb-server and ovs-vswitchd, which is required in order to
	// configure the bridge and gateway.
	runWorkerOnce()

	for {
		err := setupBridge()
		if err == nil {
			break
		}
		log.WithError(err).Warnf("Failed to exec in %s.", OvsvswitchdName)
		time.Sleep(5 * time.Second)
	}

	ip := net.IPNet{IP: ipdef.GatewayIP, Mask: ipdef.KeldaSubnet.Mask}
	for {
		err := cfgGateway(ipdef.KeldaBridge, ip)
		if err == nil {
			break
		}
		log.WithError(err).Errorf("Failed to configure %s.", ipdef.KeldaBridge)
		time.Sleep(5 * time.Second)
	}
}

func runWorkerSystem() {
	loopLog := util.NewEventTimer("Supervisor")
	for range conn.Trigger(db.MinionTable, db.EtcdTable).C {
		loopLog.LogStart()
		runWorkerOnce()
		loopLog.LogEnd()
	}
}

func runWorkerOnce() {
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
			Name:        OvsvswitchdName,
			Image:       ovsImage,
			Args:        []string{"ovs-vswitchd"},
			VolumesFrom: []string{"minion"},
			Privileged:  true,
		},
	}

	if len(etcdIPs) != 0 {
		desiredContainers = append(desiredContainers, etcdContainer(
			"--initial-cluster="+initialClusterString(etcdIPs),
			"--heartbeat-interval="+etcdHeartbeatInterval,
			"--election-timeout="+etcdElectionTimeout,
			"--proxy=on"))
	}

	if minion.PrivateIP != "" && etcdRow.LeaderIP != "" {
		err := cfgOVN(minion.PrivateIP, etcdRow.LeaderIP)
		if err == nil {
			desiredContainers = append(desiredContainers, docker.RunOptions{
				Name:        OvncontrollerName,
				Image:       ovsImage,
				Args:        []string{"ovn-controller"},
				VolumesFrom: []string{"minion"},
			})
		} else {
			log.WithError(err).Error("Failed to configure OVN")
		}

		leaderAddr := fmt.Sprintf("https://%s:6443", etcdRow.LeaderIP)
		kubeconfig := kubernetes.NewKubeconfig(leaderAddr)
		kubeconfigBytes, err := clientcmd.Write(kubeconfig)
		if err == nil {
			desiredContainers = append(desiredContainers, docker.RunOptions{
				PidMode:     "host",
				Name:        KubeletName,
				Image:       kubeImage,
				Privileged:  true,
				VolumesFrom: []string{"minion"},
				Mounts: []dkc.HostMount{
					{
						Source: "/dev",
						Target: "/dev",
						Type:   "bind",
					}, {
						Source: "/sys",
						Target: "/sys",
						Type:   "bind",
					}, {
						Source: "/var/run",
						Target: "/var/run",
						Type:   "bind",
					}, {
						Source: "/var/lib/docker",
						Target: "/var/lib/docker",
						Type:   "bind",
					}, {
						Source: "/var/lib/kubelet/",
						Target: "/var/lib/kubelet",
						Type:   "bind",
						// The Kubelet sometimes creates mounts
						// inside /var/lib/kubelet (e.g. it
						// creates a tmpfs mount for secret
						// volumes).
						// In order for the mount to propagate
						// to other containers, the propagation
						// type must be set to "shared".
						BindOptions: &dkc.BindOptions{
							Propagation: "shared",
						},
					}, {
						Source: cliPath.MinionTLSDir,
						Target: cliPath.MinionTLSDir,
						Type:   "bind",
					},
				},
				Args: kubeletArgs(minion.PrivateIP),
				FilepathToContent: map[string]string{
					"/var/lib/kubelet/kubeconfig": string(
						kubeconfigBytes),
				},
			})
		} else {
			log.WithError(err).Error("Failed to generate Kubeconfig")
		}
	}

	joinContainers(desiredContainers)
}

func kubeletArgs(myIP string) []string {
	return []string{"kubelet",
		"--pod-cidr=10.0.0.0/24",
		"--network-plugin=cni",
		"--resolv-conf=/kelda_resolv.conf",
		"--make-iptables-util-chains=false",
		"--kubeconfig=/var/lib/kubelet/kubeconfig",
		"--hostname-override", myIP,
		"--anonymous-auth=false",
		"--client-ca-file", tlsIO.CACertPath(cliPath.MinionTLSDir),
		"--tls-cert-file", tlsIO.SignedCertPath(cliPath.MinionTLSDir),
		"--tls-private-key-file", tlsIO.SignedKeyPath(cliPath.MinionTLSDir),
	}
}

func cfgOVNImpl(myIP, leaderIP string) error {
	// The values in the conf map must match the exact output of `ovs-vsctl get`.
	// Therefore, although most of the values are quoted, ovn-encap-type
	// is not.
	conf := []struct{ key, val string }{
		{"external_ids:ovn-remote", fmt.Sprintf(`"tcp:%s:6640"`, leaderIP)},
		{"external_ids:ovn-encap-ip", fmt.Sprintf("%q", myIP)},
		{"external_ids:ovn-encap-type", tunnelingProtocol},
		{"external_ids:api_server", fmt.Sprintf(`"http://%s:9000"`, leaderIP)},
		{"external_ids:system-id", fmt.Sprintf("%q", myIP)},
	}

	var expOutput string
	getCmd := []string{"--if-exists", "get", "Open_vSwitch", "."}
	setCmd := []string{"set", "Open_vSwitch", "."}
	for _, kv := range conf {
		expOutput += kv.val + "\n"
		getCmd = append(getCmd, kv.key)
		setCmd = append(setCmd, fmt.Sprintf("%s=%s", kv.key, kv.val))
	}

	actualOutput, err := execRun("ovs-vsctl", getCmd...)
	if err != nil {
		return fmt.Errorf("get OVN config: %s", err)
	}

	if string(actualOutput) != expOutput {
		c.Inc("Update OVN config")
		if _, err = execRun("ovs-vsctl", setCmd...); err != nil {
			return fmt.Errorf("set OVN config: %s", err)
		}
	}
	return nil
}

func setupBridge() error {
	gwMac := ipdef.IPToMac(ipdef.GatewayIP)
	_, err := execRun("ovs-vsctl", "add-br", ipdef.KeldaBridge,
		"--", "set", "bridge", ipdef.KeldaBridge, "fail_mode=secure",
		fmt.Sprintf("other_config:hwaddr=\"%s\"", gwMac))
	return err
}

func cfgGatewayImpl(name string, ip net.IPNet) error {
	link, err := nl.N.LinkByName(name)
	if err != nil {
		return fmt.Errorf("no such interface: %s (%s)", name, err)
	}

	if err := nl.N.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up link: %s (%s)", name, err)
	}

	if err := nl.N.AddrAdd(link, ip); err != nil {
		return fmt.Errorf("failed to set address: %s (%s)", name, err)
	}

	return nil
}

var cfgGateway = cfgGatewayImpl
var cfgOVN = cfgOVNImpl
