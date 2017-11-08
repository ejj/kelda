package supervisor

import (
	"fmt"
	"net"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/nl"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/kelda/kelda/util"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
)

func runWorker() {
	setupWorker()
	go runWorkerSystem()
}

func setupWorker() {
	run(images.Ovsdb, "ovsdb-server")
	run(images.Ovsvswitchd, "ovs-vswitchd")

	for {
		err := setupBridge()
		if err == nil {
			break
		}
		log.WithError(err).Warnf("Failed to exec in %s.", images.Ovsvswitchd)
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
	leaderIP := etcdRow.LeaderIP
	IP := minion.PrivateIP

	if !str.SliceEq(oldEtcdIPs, etcdIPs) {
		c.Inc("Reset Etcd")
		Remove(images.Etcd)
	}

	oldEtcdIPs = etcdIPs

	run(images.Etcd,
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		"--heartbeat-interval="+etcdHeartbeatInterval,
		"--election-timeout="+etcdElectionTimeout,
		"--proxy=on")

	run(images.Ovsdb, "ovsdb-server")
	run(images.Ovsvswitchd, "ovs-vswitchd")

	if leaderIP == "" || IP == "" {
		return
	}
	c.Inc("Update OVS IPs")

	err := execRun("ovs-vsctl", "set", "Open_vSwitch", ".",
		fmt.Sprintf("external_ids:ovn-remote=\"tcp:%s:6640\"", leaderIP),
		fmt.Sprintf("external_ids:ovn-encap-ip=%s", IP),
		fmt.Sprintf("external_ids:ovn-encap-type=\"%s\"", tunnelingProtocol),
		fmt.Sprintf("external_ids:api_server=\"http://%s:9000\"", leaderIP),
		fmt.Sprintf("external_ids:system-id=\"%s\"", IP))
	if err != nil {
		log.WithError(err).Warnf("Failed to exec in %s.", images.Ovsvswitchd)
		return
	}

	run(images.Ovncontroller, "ovn-controller")
}

func setupBridge() error {
	gwMac := ipdef.IPToMac(ipdef.GatewayIP)
	return execRun("ovs-vsctl", "add-br", ipdef.KeldaBridge,
		"--", "set", "bridge", ipdef.KeldaBridge, "fail_mode=secure",
		fmt.Sprintf("other_config:hwaddr=\"%s\"", gwMac))
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
