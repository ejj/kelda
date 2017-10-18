package cloud

import (
	"errors"
	"fmt"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/foreman"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	log "github.com/sirupsen/logrus"
)

var isConnected = foreman.IsConnected

type joinResult struct {
	acls []acl.ACL

	boot      []db.Machine
	terminate []db.Machine
	updateIPs []db.Machine
}

var cloudJoin = joinImpl

func joinImpl(cld cloud) (joinResult, error) {
	res := joinResult{}

	machines, err := cld.provider.List()
	if err != nil {
		log.WithError(err).Error("Failed to list machines")
		return res, err
	}
	machines = getMachineRoles(machines)

	err = cld.conn.Txn(db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {
		bp, err := view.GetBlueprint()
		if err != nil {
			log.WithError(err).Error("Failed to get blueprint")
			return err
		}

		if cld.namespace != bp.Namespace {
			err := errors.New("namespace change during a cloud run")
			log.WithError(err).Debug("Cloud run abort")
			return err
		}

		cld.updateDBMachines(view, machines)
		res.boot, res.terminate, res.updateIPs = cld.planUpdates(view)

		// Regions with no machines in them should have their ACLs cleared.
		if len(machines) > 0 {
			for acl := range cld.desiredACLs(bp) {
				res.acls = append(res.acls, acl)
			}
		}

		return nil
	})
	return res, err
}

func (cld cloud) updateDBMachines(view db.Database, cloudMachines []db.Machine) {
	dbms := cld.selectMachines(view)

	pairs, dbmis, cmis := join.Join(dbms, cloudMachines, machineScore)

	for _, cmi := range cmis {
		pairs = append(pairs, join.Pair{L: view.InsertMachine(), R: cmi})
	}

	for _, dbmi := range dbmis {
		view.Remove(dbmi.(db.Machine))
	}

	for _, pair := range pairs {
		dbm := pair.L.(db.Machine)
		cm := pair.R.(db.Machine)

		// Providers don't know about some fields, so we don't overwrite them.
		cm.ID = dbm.ID
		cm.Status = dbm.Status
		cm.SSHKeys = dbm.SSHKeys
		view.Commit(cm)
	}
}

func (cld cloud) planUpdates(view db.Database) (boot, stop, updateIPs []db.Machine) {
	bp, err := view.GetBlueprint()
	if err != nil {
		// Already got the blueprint earlier in this transaction.
		panic(fmt.Sprintf("Unreachable error: %v", err))
	}

	bpms := cld.desiredMachines(bp.Blueprint.Machines)
	dbms := cld.selectMachines(view)

	pairs, bpmis, dbmis := join.Join(bpms, dbms, machineScore)

	for _, p := range pairs {
		bpm := p.L.(db.Machine)
		dbm := p.R.(db.Machine)

		status := connectionStatus(dbm)
		if status != "" {
			dbm.Status = status
			view.Commit(dbm)
		}

		if bpm.FloatingIP != dbm.FloatingIP {
			dbm.FloatingIP = bpm.FloatingIP
			updateIPs = append(updateIPs, dbm)
		}
	}

	for _, dbmi := range dbmis {
		dbm := dbmi.(db.Machine)
		dbm.Status = db.Stopping
		view.Commit(dbm)

		stop = append(stop, dbm)
	}

	for _, bpmi := range bpmis {
		bpm := bpmi.(db.Machine)
		dbm := view.InsertMachine()
		bpm.ID = dbm.ID
		bpm.Status = db.Booting
		view.Commit(bpm)

		boot = append(boot, bpm)
	}

	return
}

func machineScore(left, right interface{}) int {
	l := left.(db.Machine)
	r := right.(db.Machine)

	switch {
	case l.Provider != r.Provider || l.Region != r.Region:
		// The caller should assure that this condition is met between all pairs.
		panic("Invalid Provider or Region")
	case l.Preemptible != r.Preemptible:
		return -1
	case l.Size != r.Size:
		return -1
	case l.DiskSize != 0 && r.DiskSize != 0 && l.DiskSize != r.DiskSize:
		return -1
	case l.Role != db.None && r.Role != db.None && l.Role != r.Role:
		return -1
	case l.CloudID != "" && r.CloudID != "" && l.CloudID == r.CloudID:
		return 0
	case l.FloatingIP != "" && r.FloatingIP != "" && l.FloatingIP == r.FloatingIP:
		return 1
	case l.Role != db.None && r.Role != db.None:
		return 2 // Prefer to match pairs that have a role assigned.
	default:
		return 3
	}
}

func (cld cloud) desiredMachines(bpms []blueprint.Machine) []db.Machine {
	var dbms []db.Machine
	for _, bpm := range bpms {
		region := bpm.Region
		if bpm.Provider != string(cld.providerName) || region != cld.region {
			continue
		}

		role, err := db.ParseRole(bpm.Role)
		if err != nil {
			log.WithError(err).Error("Parse error: ", bpm.Role)
			continue
		}

		dbm := db.Machine{
			Region:      region,
			FloatingIP:  bpm.FloatingIP,
			Role:        role,
			Provider:    db.ProviderName(bpm.Provider),
			Preemptible: bpm.Preemptible,
			Size:        bpm.Size,
			DiskSize:    bpm.DiskSize,
			SSHKeys:     bpm.SSHKeys,
		}

		if dbm.DiskSize == 0 {
			dbm.DiskSize = defaultDiskSize
		}

		if adminKey != "" {
			dbm.SSHKeys = append(dbm.SSHKeys, adminKey)
		}

		dbms = append(dbms, dbm)
	}
	return dbms
}

func connectionStatus(m db.Machine) string {
	// "Connected" takes priority over other statuses.
	connected := m.PublicIP != "" && isConnected(m.CloudID)
	if connected {
		return db.Connected
	}

	// If we had previously connected, and we are not currently connected, show
	// that we are attempting to reconnect.
	if m.Status == db.Connected || m.Status == db.Reconnecting {
		return db.Reconnecting
	}

	// If we've never successfully connected, but have booted enough to have a
	// public IP, show that we are attempting to connect.
	if m.PublicIP != "" {
		return db.Connecting
	}

	return ""
}

func (cld cloud) desiredACLs(bp db.Blueprint) map[acl.ACL]struct{} {
	aclSet := map[acl.ACL]struct{}{}

	// Always allow traffic from the Kelda controller, so we append local.
	for _, cidr := range append(bp.AdminACL, "local") {
		acl := acl.ACL{
			CidrIP:  cidr,
			MinPort: 1,
			MaxPort: 65535,
		}
		aclSet[acl] = struct{}{}
	}

	for _, conn := range bp.Connections {
		if conn.From == blueprint.PublicInternetLabel {
			acl := acl.ACL{
				CidrIP:  "0.0.0.0/0",
				MinPort: conn.MinPort,
				MaxPort: conn.MaxPort,
			}
			aclSet[acl] = struct{}{}
		}
	}

	return aclSet
}

func (cld cloud) selectMachines(view db.Database) []db.Machine {
	return view.SelectFromMachine(func(dbm db.Machine) bool {
		return dbm.Provider == cld.providerName && dbm.Region == cld.region
	})
}

func getMachineRoles(machines []db.Machine) (withRoles []db.Machine) {
	for _, m := range machines {
		m.Role = getMachineRole(m.CloudID)
		withRoles = append(withRoles, m)
	}
	return withRoles
}
