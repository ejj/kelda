package supervisor

import (
	"fmt"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/kelda/kelda/util"
	"github.com/kelda/kelda/util/str"
)

func runMaster() {
	run(images.Ovsdb, "ovsdb-server")
	run(images.Registry)
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

	IP := minion.PrivateIP
	etcdIPs := etcdRow.EtcdIPs
	leader := etcdRow.Leader

	if oldIP != IP || !str.SliceEq(oldEtcdIPs, etcdIPs) {
		c.Inc("Reset Etcd")
		Remove(images.Etcd)
	}

	oldEtcdIPs = etcdIPs
	oldIP = IP

	if IP == "" || len(etcdIPs) == 0 {
		return
	}

	run(images.Etcd, fmt.Sprintf("--name=master-%s", IP),
		fmt.Sprintf("--initial-cluster=%s", initialClusterString(etcdIPs)),
		fmt.Sprintf("--advertise-client-urls=http://%s:2379", IP),
		fmt.Sprintf("--listen-peer-urls=http://%s:2380", IP),
		fmt.Sprintf("--initial-advertise-peer-urls=http://%s:2380", IP),
		"--listen-client-urls=http://0.0.0.0:2379",
		"--heartbeat-interval="+etcdHeartbeatInterval,
		"--initial-cluster-state=new",
		"--election-timeout="+etcdElectionTimeout)

	run(images.Ovsdb, "ovsdb-server")
	run(images.Registry)

	if leader {
		/* XXX: If we fail to boot ovn-northd, we should give up
		* our leadership somehow.  This ties into the general
		* problem of monitoring health. */
		run(images.Ovnnorthd, "ovn-northd")
	} else {
		Remove(images.Ovnnorthd)
	}
}
