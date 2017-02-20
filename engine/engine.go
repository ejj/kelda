package engine

import (
	"github.com/quilt/quilt/cluster"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/stitch"
	"github.com/quilt/quilt/util"

	log "github.com/Sirupsen/logrus"
)

var myIP = util.MyIP
var defaultDiskSize = 32

// Run updates the database in response to stitch changes in the cluster table.
func Run(conn db.Conn) {
	for range conn.TriggerTick(30, db.ClusterTable, db.MachineTable, db.ACLTable).C {
		conn.Txn(db.ACLTable, db.ClusterTable,
			db.MachineTable).Run(updateTxn)
	}
}

func updateTxn(view db.Database) error {
	cluster, err := view.GetCluster()
	if err != nil {
		return err
	}

	stitch, err := stitch.FromJSON(cluster.Spec)
	if err != nil {
		return err
	}

	cluster.Namespace = stitch.Namespace
	view.Commit(cluster)

	machineTxn(view, stitch)
	aclTxn(view, stitch)
	return nil
}

func aclTxn(view db.Database, specHandle stitch.Stitch) {
	aclRow, err := view.GetACL()
	if err != nil {
		aclRow = view.InsertACL()
	}

	aclRow.Admin = resolveACLs(specHandle.AdminACL)

	var applicationPorts []db.PortRange
	for _, conn := range specHandle.Connections {
		if conn.From == stitch.PublicInternetLabel {
			applicationPorts = append(applicationPorts, db.PortRange{
				MinPort: conn.MinPort,
				MaxPort: conn.MaxPort,
			})
		}
	}
	aclRow.ApplicationPorts = applicationPorts

	view.Commit(aclRow)
}

// toDBMachine converts machines specified in the Stitch into db.Machines that can
// be compared against what's already in the db.
// Specifically, it sets the role of the db.Machine, the size (which may depend
// on RAM and CPU constraints), and the provider.
// Additionally, it skips machines with invalid roles, sizes or providers.
func toDBMachine(machines []stitch.Machine, maxPrice float64) []db.Machine {
	var hasMaster, hasWorker bool
	var dbMachines []db.Machine
	for _, stitchm := range machines {
		var m db.Machine

		role, err := db.ParseRole(stitchm.Role)
		if err != nil {
			log.WithError(err).Error("Error parsing role.")
			continue
		}
		m.Role = role

		hasMaster = hasMaster || role == db.Master
		hasWorker = hasWorker || role == db.Worker

		p, err := db.ParseProvider(stitchm.Provider)
		if err != nil {
			log.WithError(err).Error("Error parsing provider.")
			continue
		}
		m.Provider = p
		m.Size = stitchm.Size

		if m.Size == "" {
			m.Size = cluster.ChooseSize(p, stitchm.RAM, stitchm.CPU,
				maxPrice)
			if m.Size == "" {
				log.Errorf("No valid size for %v, skipping.", m)
				continue
			}
		}

		m.DiskSize = stitchm.DiskSize
		if m.DiskSize == 0 {
			m.DiskSize = defaultDiskSize
		}

		m.StitchID = stitchm.ID
		m.SSHKeys = stitchm.SSHKeys
		m.Region = stitchm.Region
		m.FloatingIP = stitchm.FloatingIP
		dbMachines = append(dbMachines, cluster.DefaultRegion(m))
	}

	if hasMaster && !hasWorker {
		log.Warning("A Master was specified but no workers.")
		return nil
	} else if hasWorker && !hasMaster {
		log.Warning("A Worker was specified but no masters.")
		return nil
	}

	return dbMachines
}

func machineTxn(view db.Database, stitch stitch.Stitch) {
	// XXX: How best to deal with machines that don't specify enough information?
	maxPrice := stitch.MaxPrice
	stitchMachines := toDBMachine(stitch.Machines, maxPrice)

	dbMachines := view.SelectFromMachine(nil)

	scoreFun := func(left, right interface{}) int {
		stitchMachine := left.(db.Machine)
		dbMachine := right.(db.Machine)

		switch {
		case dbMachine.StitchID != "" &&
			dbMachine.StitchID != stitchMachine.StitchID:
			return -1
		case dbMachine.Provider != stitchMachine.Provider:
			return -1
		case dbMachine.Region != stitchMachine.Region:
			return -1
		case dbMachine.Size != "" && stitchMachine.Size != dbMachine.Size:
			return -1
		case dbMachine.FloatingIP != "" &&
			dbMachine.FloatingIP != stitchMachine.FloatingIP:
			return -1
		case dbMachine.Role != db.None && dbMachine.Role != stitchMachine.Role:
			return -1
		case dbMachine.DiskSize != stitchMachine.DiskSize:
			return -1
		case dbMachine.PrivateIP == "":
			return 2
		case dbMachine.PublicIP == "":
			return 1
		default:
			return 0
		}
	}

	pairs, bootList, terminateList := join.Join(stitchMachines, dbMachines, scoreFun)

	for _, toTerminate := range terminateList {
		toTerminate := toTerminate.(db.Machine)
		view.Remove(toTerminate)
	}

	for _, bootSet := range bootList {
		bootSet := bootSet.(db.Machine)

		pairs = append(pairs, join.Pair{L: bootSet, R: view.InsertMachine()})
	}

	for _, pair := range pairs {
		stitchMachine := pair.L.(db.Machine)
		dbMachine := pair.R.(db.Machine)

		dbMachine.StitchID = stitchMachine.StitchID
		dbMachine.Role = stitchMachine.Role
		dbMachine.Size = stitchMachine.Size
		dbMachine.DiskSize = stitchMachine.DiskSize
		dbMachine.Provider = stitchMachine.Provider
		dbMachine.Region = stitchMachine.Region
		dbMachine.SSHKeys = stitchMachine.SSHKeys
		dbMachine.FloatingIP = stitchMachine.FloatingIP
		view.Commit(dbMachine)
	}
}

func resolveACLs(acls []string) []string {
	var result []string
	for _, acl := range acls {
		if acl == "local" {
			ip, err := myIP()
			if err != nil {
				log.WithError(err).Warn("Failed to get IP address.")
				continue
			}
			acl = ip + "/32"
		}
		result = append(result, acl)
	}

	return result
}
