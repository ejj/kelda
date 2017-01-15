// Package network manages the network services of the application dataplane.  This
// means ensuring that containers can find and communicate with each other in accordance
// with the policy specification.  It achieves this by manipulating IP addresses and
// hostnames within the containers, Open vSwitch on each running worker, and the OVN
// controller.
package network

import (
	"fmt"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/ovsdb"
	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
)

const labelMac = "0a:00:00:00:00:00"
const lSwitch = "quilt"
const quiltBridge = "quilt-int"
const ovnBridge = "br-int"

type dbport struct {
	bridge string
	ip     string
	mac    string
}

// dbslice is a wrapper around []dbport to allow us to perform a join
type dbslice []dbport

// Run blocks implementing the network services.
func Run(conn db.Conn) {
	loopLog := util.NewEventTimer("Network")
	for range conn.TriggerTick(30, db.MinionTable, db.ContainerTable,
		db.ConnectionTable, db.LabelTable, db.EtcdTable).C {

		loopLog.LogStart()
		runDNS(conn)
		runWorker(conn)
		runMaster(conn)
		loopLog.LogEnd()
	}
}

// The leader of the cluster is responsible for properly configuring OVN northd for
// container networking.  This simply means creating a logical port for each container
// and label.  The specialized OpenFlow rules Quilt requires are managed by the workers
// individuallly.
func runMaster(conn db.Conn) {
	var leader, init bool
	var labels []db.Label
	var containers []db.Container
	var connections []db.Connection
	conn.Txn(db.ConnectionTable, db.ContainerTable, db.EtcdTable,
		db.LabelTable, db.MinionTable).Run(func(view db.Database) error {

		init = checkSupervisorInit(view)
		leader = view.EtcdLeader()

		labels = view.SelectFromLabel(func(label db.Label) bool {
			return label.IP != ""
		})

		containers = view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.IP != ""
		})

		connections = view.SelectFromConnection(nil)
		return nil
	})

	if !init || !leader {
		return
	}

	var dbData []dbport
	for _, l := range labels {
		if l.MultiHost {
			dbData = append(dbData, dbport{
				bridge: lSwitch,
				ip:     l.IP,
				mac:    labelMac,
			})
		}
	}
	for _, c := range containers {
		dbData = append(dbData, dbport{bridge: lSwitch, ip: c.IP,
			mac: ipdef.IPStrToMac(c.IP)})
	}

	ovsdbClient, err := ovsdb.Open()
	if err != nil {
		log.WithError(err).Error("Failed to connect to OVSDB.")
		return
	}
	defer ovsdbClient.Close()

	ovsdbClient.CreateLogicalSwitch(lSwitch)
	lports, err := ovsdbClient.ListLogicalPorts(lSwitch)
	if err != nil {
		log.WithError(err).Error("Failed to list OVN ports.")
		return
	}

	portKey := func(val interface{}) interface{} {
		port := val.(ovsdb.LPort)
		return fmt.Sprintf("bridge:%s\nname:%s", port.Bridge, port.Name)
	}

	dbKey := func(val interface{}) interface{} {
		dbPort := val.(dbport)
		return fmt.Sprintf("bridge:%s\nname:%s", dbPort.bridge, dbPort.ip)
	}

	_, ovsps, dbps := join.HashJoin(ovsdb.LPortSlice(lports), dbslice(dbData),
		portKey, dbKey)

	for _, dbp := range dbps {
		lport := dbp.(dbport)
		log.WithField("IP", lport.ip).Info("New logical port.")
		err := ovsdbClient.CreateLogicalPort(lport.bridge, lport.ip, lport.mac,
			lport.ip)
		if err != nil {
			log.WithError(err).Warnf("Failed to create port %s.", lport.ip)
		}
	}

	for _, ovsp := range ovsps {
		lport := ovsp.(ovsdb.LPort)
		log.Infof("Delete logical port %s.", lport.Name)
		if err := ovsdbClient.DeleteLogicalPort(lSwitch, lport); err != nil {
			log.WithError(err).Warn("Failed to delete logical port.")
		}
	}

	updateACLs(ovsdbClient, connections, labels)
}

// Len returns the length of the slice
func (dbs dbslice) Len() int {
	return len(dbs)
}

// Get returns the element at index i of the slice
func (dbs dbslice) Get(i int) interface{} {
	return dbs[i]
}

func checkSupervisorInit(view db.Database) bool {
	self, err := view.MinionSelf()
	return err == nil && self.SupervisorInit
}
