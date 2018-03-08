package cloud

import (
	"errors"
	"fmt"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/foreman"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util/str"

	log "github.com/sirupsen/logrus"
)

var isConnected = foreman.IsConnected

type joinResult struct {
	acls []acl.ACL

	boot      []db.Machine
	terminate []db.Machine
	updateIPs []db.Machine

	// True if there's things going on in this join that warrant frequent polls.
	isActive bool
}

var cloudJoin = joinImpl

func joinImpl(cld *cloud) (joinResult, error) {
	machines, err := cld.provider.List()
	if err != nil {
		log.WithError(err).Error("Failed to list machines")
		return joinResult{}, err
	}
	machines = getMachineRoles(machines)

	var res joinResult
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

		cld.syncDBWithCloud(view, machines)
		res = cld.syncDBWithBlueprint(view)

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

// syncDBWithCloud updates the machines in the database, based on the ground truth
// from the cloud provider about which machines are running.
func (cld *cloud) syncDBWithCloud(view db.Database, cloudMachines []db.Machine) {
	dbms := cld.selectMachines(view)

	pairs, extraDBMs, missingCMs := join.Join(dbms, cloudMachines, machineScore)

	for _, cm := range missingCMs {
		pairs = append(pairs, join.Pair{L: view.InsertMachine(), R: cm})
	}

	for _, dbm := range extraDBMs {
		view.Remove(dbm.(db.Machine))
	}

	for _, pair := range pairs {
		dbm := pair.L.(db.Machine)
		cm := pair.R.(db.Machine)

		// Providers don't know about some fields, so we don't overwrite them.
		cm.ID = dbm.ID
		cm.Status = dbm.Status
		cm.SSHKeys = dbm.SSHKeys
		cm.PublicKey = dbm.PublicKey
		view.Commit(cm)
	}
}

func (cld *cloud) syncDBWithBlueprint(view db.Database) joinResult {
	var res joinResult

	bp, err := view.GetBlueprint()
	if err != nil {
		// Already got the blueprint earlier in this transaction.
		panic(fmt.Sprintf("Unreachable error: %v", err))
	}

	dbms := cld.selectMachines(view)

	bpms := cld.desiredMachines(bp.Blueprint.Machines)
	if len(bpms) > 0 || len(dbms) > 0 {
		res.isActive = true
	}

	pairs, missingBPMs, extraDBMs := join.Join(bpms, dbms, machineScore)

	for _, p := range pairs {
		bpm := p.L.(db.Machine)
		dbm := p.R.(db.Machine)

		// Write the SSH keys into the database machine to ensure that the
		// database contains the most up to date SSH keys. If we didn't,
		// changes to the SSH keys in the blueprint would never get synced.
		// Similarly, the SSH keys would not be properly synced if the daemon
		// restarted when machines were already running in the cloud.
		dbm.SSHKeys = bpm.SSHKeys
		status := connectionStatus(dbm)
		if status != "" {
			dbm.Status = status
		}
		view.Commit(dbm)

		// Only update IPs once the roles are set. This way, we avoid assigning
		// a floating IP to a machine that is going to connect with the other
		// role, which will require reassigning the IP. This also prevents the
		// cloud code from attempting to assign floating IPs while machines are
		// still booting, which can be a problem for Amazon preemptible
		// instances.
		if dbm.Role != db.None && bpm.FloatingIP != dbm.FloatingIP {
			dbm.FloatingIP = bpm.FloatingIP
			res.updateIPs = append(res.updateIPs, dbm)
		}
	}

	for _, extraDBM := range extraDBMs {
		dbm := extraDBM.(db.Machine)
		dbm.Status = db.Stopping
		view.Commit(dbm)

		res.terminate = append(res.terminate, dbm)
	}

	for _, missingBPM := range missingBPMs {
		bpm := missingBPM.(db.Machine)
		dbm := view.InsertMachine()
		bpm.ID = dbm.ID
		bpm.Status = db.Booting

		res.boot = append(res.boot, bpm)

		// Don't bother assigning the role to the database, as the foreman will
		// just unassign it in the next run loop.  We can't be sure which machine
		// in the database gets which role until we actually connect to it
		// anyways.
		bpm.Role = db.None
		view.Commit(bpm)
	}

	return res
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
	}

	score := 10

	// Moving a floating IP is a major change, so strongly prefer leaving it.
	// There's some risk that the floating IP is attached to a machine with the wrong
	// role.  Normally that would be fine because the above switch statement would
	// disqualify the match, but if we don't know the machine's role yet, an
	// erroneous match could be made.  In rare cases, this could cause us to stop
	// machines unnecessarily as the mismatched role would make proper matches
	// impossible down the line.  Currently, we find this trade-off acceptable as
	// practically speaking floating IPs are always assigned to workers, and the join
	// code doesn't guarantee optimal matches anyways.
	if l.FloatingIP != "" && r.FloatingIP != "" && l.FloatingIP == r.FloatingIP {
		score -= 2
	}

	// Pairs with matching roles should be prioritized over pairs with matching
	// floating IPs.
	if l.Role != db.None && r.Role != db.None {
		score--
	}
	return score
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

func (cld *cloud) desiredACLs(bp db.Blueprint) map[acl.ACL]struct{} {
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
		if str.SliceContains(conn.From, blueprint.PublicInternetLabel) {
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

func (cld *cloud) selectMachines(view db.Database) []db.Machine {
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
