package cloud

import (
	"errors"
	"fmt"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"

	log "github.com/sirupsen/logrus"
)

type joinResult struct {
	acls []acl.ACL

	boot      []db.Machine
	terminate []db.Machine
	updateIPs []db.Machine
}

func (cld cloud) join() (joinResult, error) {
	res := joinResult{}

	cloudMachines, err := cld.get()
	if err != nil {
		log.WithError(err).Error("Failed to list machines")
		return res, err
	}

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

		machines := view.SelectFromMachine(func(m db.Machine) bool {
			return m.Provider == cld.providerName && m.Region == cld.region
		})

		cloudMachines = getMachineRoles(cloudMachines)

		dbResult := syncDB(cloudMachines, machines)
		res.boot = dbResult.boot
		res.terminate = dbResult.stop
		res.updateIPs = dbResult.updateIPs

		for _, dbm := range res.boot {
			dbm.Status = db.Booting
			view.Commit(dbm)
		}

		for _, pair := range dbResult.pairs {
			dbm := pair.L.(db.Machine)
			m := pair.R.(db.Machine)

			if m.Role != db.None && m.Role == dbm.Role {
				dbm.CloudID = m.CloudID
			}

			if dbm.PublicIP != m.PublicIP {
				// We're changing the association between a database
				// machine and a cloud machine, so the status is not
				// applicable.
				dbm.Status = ""
			}
			dbm.PublicIP = m.PublicIP
			dbm.PrivateIP = m.PrivateIP

			view.Commit(dbm)
		}

		// Regions with no machines in them should have their ACLs cleared.
		if len(machines) > 0 {
			for acl := range cld.getACLs(bp) {
				res.acls = append(res.acls, acl)
			}
		}

		return nil
	})
	return res, err
}

func (cld cloud) getACLs(bp db.Blueprint) map[acl.ACL]struct{} {
	aclSet := map[acl.ACL]struct{}{}

	// Always allow traffic from the Quilt controller, so we append local.
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

type syncDBResult struct {
	pairs     []join.Pair
	boot      []db.Machine
	stop      []db.Machine
	updateIPs []db.Machine
}

func syncDB(cms []db.Machine, dbms []db.Machine) syncDBResult {
	ret := syncDBResult{}

	pair1, dbmis, cmis := join.Join(dbms, cms, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(db.Machine)

		if dbm.CloudID == m.CloudID && dbm.Provider == m.Provider &&
			dbm.Preemptible == m.Preemptible &&
			dbm.Region == m.Region && dbm.Size == m.Size &&
			(m.DiskSize == 0 || dbm.DiskSize == m.DiskSize) &&
			(m.Role == db.None || dbm.Role == m.Role) {
			return 0
		}

		return -1
	})

	pair2, dbmis, cmis := join.Join(dbmis, cmis, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(db.Machine)

		if dbm.Provider != m.Provider ||
			dbm.Region != m.Region ||
			dbm.Size != m.Size ||
			dbm.Preemptible != m.Preemptible ||
			(m.DiskSize != 0 && dbm.DiskSize != m.DiskSize) ||
			(m.Role != db.None && dbm.Role != m.Role) {
			return -1
		}

		score := 10
		if dbm.Role != db.None && m.Role != db.None && dbm.Role == m.Role {
			score -= 4
		}
		if dbm.PublicIP == m.PublicIP && dbm.PrivateIP == m.PrivateIP {
			score -= 2
		}
		if dbm.FloatingIP == m.FloatingIP {
			score--
		}
		return score
	})

	for _, cm := range cmis {
		ret.stop = append(ret.stop, cm.(db.Machine))
	}

	for _, dbm := range dbmis {
		m := dbm.(db.Machine)
		ret.boot = append(ret.boot, m)
	}

	for _, pair := range append(pair1, pair2...) {
		dbm := pair.L.(db.Machine)
		m := pair.R.(db.Machine)

		if dbm.CloudID == m.CloudID && dbm.FloatingIP != m.FloatingIP {
			m.FloatingIP = dbm.FloatingIP
			ret.updateIPs = append(ret.updateIPs, m)
		}

		ret.pairs = append(ret.pairs, pair)
	}

	return ret
}

func (cld cloud) get() ([]db.Machine, error) {
	c.Inc("List")

	machines, err := cld.provider.List()
	if err != nil {
		return nil, fmt.Errorf("list %s: %s", cld, err)
	}

	var cloudMachines []db.Machine
	for _, m := range machines {
		m.Provider = cld.providerName
		m.Region = cld.region
		cloudMachines = append(cloudMachines, m)
	}
	return cloudMachines, nil
}

func getMachineRoles(machines []db.Machine) (withRoles []db.Machine) {
	for _, m := range machines {
		m.Role = getMachineRole(m.PublicIP)
		withRoles = append(withRoles, m)
	}
	return withRoles
}
