// Package network manages the network services of the application dataplane.  This
// means ensuring that containers can find and communicate with each other in accordance
// with the policy specification.  It achieves this by manipulating IP addresses and
// hostnames within the containers, Open vSwitch on each running worker, and the OVN
// controller.
package network

import (
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/minion/ipdef"
	"github.com/quilt/quilt/minion/ovsdb"

	log "github.com/Sirupsen/logrus"
)

const labelMac = "0a:00:00:00:00:00"
const lSwitch = "quilt"

// Run blocks implementing the network services.
func Run(conn db.Conn) {
	go runNat(conn)
	go runDNS(conn)
	go runUpdateIPs(conn)

	for range conn.TriggerTick(30, db.MinionTable, db.ContainerTable,
		db.ConnectionTable, db.LabelTable, db.EtcdTable).C {
		if conn.EtcdLeader() {
			runMaster(conn)
		}
	}
}

// The leader of the cluster is responsible for properly configuring OVN northd for
// container networking.  This simply means creating a logical port for each container
// and label.  The specialized OpenFlow rules Quilt requires are managed by the workers
// individuallly.
func runMaster(conn db.Conn) {
	var init bool
	var labels []db.Label
	var containers []db.Container
	var connections []db.Connection
	conn.Txn(db.ConnectionTable, db.ContainerTable, db.EtcdTable,
		db.LabelTable, db.MinionTable).Run(func(view db.Database) error {

		init = checkSupervisorInit(view)

		labels = view.SelectFromLabel(func(label db.Label) bool {
			return label.IP != ""
		})

		containers = view.SelectFromContainer(func(dbc db.Container) bool {
			return dbc.IP != ""
		})

		connections = view.SelectFromConnection(nil)
		return nil
	})

	if !init {
		return
	}

	ovsdbClient, err := ovsdb.Open()
	if err != nil {
		log.WithError(err).Error("Failed to connect to OVSDB.")
		return
	}
	defer ovsdbClient.Disconnect()

	ovsdbClient.CreateLogicalSwitch(lSwitch)
	lports, err := ovsdbClient.ListLogicalPorts()
	if err != nil {
		log.WithError(err).Error("Failed to list OVN ports.")
		return
	}

	dbcKey := func(val interface{}) interface{} {
		return val.(db.Container).IP
	}
	portKey := func(val interface{}) interface{} {
		return val.(ovsdb.LPort).Name
	}

	_, ovsps, dbcs := join.HashJoin(ovsdb.LPortSlice(lports),
		db.ContainerSlice(containers), portKey, dbcKey)

	for _, dbcIface := range dbcs {
		dbc := dbcIface.(db.Container)
		err := ovsdbClient.CreateLogicalPort(lSwitch, dbc.IP,
			ipdef.IPStrToMac(dbc.IP), dbc.IP)
		if err != nil {
			log.WithError(err).Warnf("Failed to create logical port: %s",
				dbc.IP)
		} else {
			log.Infof("New logical port: %s", dbc.IP)
		}
	}

	for _, ovsp := range ovsps {
		lport := ovsp.(ovsdb.LPort)
		if err := ovsdbClient.DeleteLogicalPort(lSwitch, lport); err != nil {
			log.WithError(err).Warnf("Failed to delete logical port: %s",
				lport.Name)
		} else {
			log.Infof("Delete logical port: %s", lport.Name)
		}
	}

	updateACLs(ovsdbClient, connections, labels)
}

func checkSupervisorInit(view db.Database) bool {
	self, err := view.MinionSelf()
	return err == nil && self.SupervisorInit
}
