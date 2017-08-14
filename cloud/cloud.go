package cloud

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/quilt/quilt/cloud/acl"
	"github.com/quilt/quilt/cloud/amazon"
	"github.com/quilt/quilt/cloud/cfg"
	"github.com/quilt/quilt/cloud/digitalocean"
	"github.com/quilt/quilt/cloud/foreman"
	"github.com/quilt/quilt/cloud/google"
	"github.com/quilt/quilt/cloud/vagrant"
	"github.com/quilt/quilt/connection"
	"github.com/quilt/quilt/counter"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/util"
)

type provider interface {
	List() ([]db.Machine, error)

	Boot([]db.Machine) error

	Stop([]db.Machine) error

	SetACLs([]acl.ACL) error

	UpdateFloatingIPs([]db.Machine) error
}

// Store the providers in a variable so we can change it in the tests
var allProviders = []db.Provider{db.Amazon, db.DigitalOcean, db.Google, db.Vagrant}

var c = counter.New("Cloud")

type launchLoc struct {
	provider db.Provider
	region   string
}

func (loc launchLoc) String() string {
	if loc.region == "" {
		return string(loc.provider)
	}
	return fmt.Sprintf("%s-%s", loc.provider, loc.region)
}

type cloud struct {
	namespace string
	conn      db.Conn
	providers map[launchLoc]provider
}

var myIP = util.MyIP
var sleep = time.Sleep

// Run continually checks 'conn' for cloud changes and recreates the cloud as
// needed.
func Run(conn db.Conn, creds connection.Credentials, minionTLSDir string) {
	cfg.MinionTLSDir = minionTLSDir

	go updateMachineStatuses(conn)
	var cld *cloud
	for range conn.TriggerTick(30, db.BlueprintTable, db.MachineTable,
		db.ACLTable).C {
		c.Inc("Run")
		cld = updateCloud(conn, cld, creds)

		// Somewhat of a crude rate-limit of once every five seconds to avoid
		// stressing out the cloud providers with too many API calls.
		sleep(5 * time.Second)
	}
}

func updateCloud(conn db.Conn, cld *cloud, creds connection.Credentials) *cloud {
	namespace, err := conn.GetBlueprintNamespace()
	if err != nil {
		return cld
	}

	if cld == nil || cld.namespace != namespace {
		cld = newCloud(conn, namespace)
		cld.runOnce()
		foreman.Init(cld.conn, creds)
	}

	cld.runOnce()
	foreman.RunOnce(cld.conn)

	return cld
}

func newCloud(conn db.Conn, namespace string) *cloud {
	cld := &cloud{
		namespace: namespace,
		conn:      conn,
		providers: make(map[launchLoc]provider),
	}

	for _, p := range allProviders {
		for _, r := range validRegions(p) {
			if _, err := cld.getProvider(launchLoc{p, r}); err != nil {
				log.Debugf("Failed to connect to provider %s in %s: %s",
					p, r, err)
			}
		}
	}

	return cld
}

func (cld cloud) runOnce() {
	/* Each iteration of this loop does the following:
	 *
	 * - Get the current set of machines and ACLs from the cloud provider.
	 * - Get the current policy from the database.
	 * - Compute a diff.
	 * - Update the cloud provider accordingly.
	 *
	 * Updating the cloud provider may have consequences (creating machines, for
	 * example) that should be reflected in the database.  Therefore, if updates
	 * are necessary, the code loops a second time so that the database can be
	 * updated before the next runOnce() call.
	 */
	for i := 0; i < 2; i++ {
		jr, err := cld.join()
		if err != nil {
			return
		}

		if len(jr.boot) == 0 &&
			len(jr.terminate) == 0 &&
			len(jr.updateIPs) == 0 {
			// ACLs must be processed after Quilt learns about what machines
			// are in the cloud.  If we didn't, inter-machine ACLs could get
			// removed when the Quilt controller restarts, even if there are
			// running cloud machines that still need to communicate.
			cld.syncACLs(jr.acl.Admin, jr.acl.ApplicationPorts, jr.machines)
			return
		}

		cld.boot(jr.boot)
		cld.updateCloud(jr.terminate, provider.Stop, "stop")
		cld.updateCloud(jr.updateIPs, provider.UpdateFloatingIPs,
			"update floating IPs")
	}
}

func (cld cloud) boot(machines []db.Machine) {
	var cloudMachines []joinMachine
	for _, m := range machines {
		cloudMachines = append(cloudMachines, joinMachine{
			Machine: db.Machine{
				Size:        m.Size,
				DiskSize:    m.DiskSize,
				Preemptible: m.Preemptible,
				SSHKeys:     m.SSHKeys,
				Role:        m.Role,
			},
			provider: m.Provider,
			region:   m.Region,
		})
	}

	setStatuses(cld.conn, machines, db.Booting)
	defer setStatuses(cld.conn, machines, "")
	cld.updateCloud(cloudMachines, provider.Boot, "boot")
}

type machineAction func(provider, []db.Machine) error

func (cld cloud) updateCloud(machines []joinMachine, fn machineAction, action string) {
	if len(machines) == 0 {
		return
	}

	log.WithFields(log.Fields{
		"count":  len(machines),
		"action": action,
	}).Info("Updating cloud")

	noFailures := true
	groupedMachines := groupByLoc(machines)
	for loc, providerMachines := range groupedMachines {
		providerInst, err := cld.getProvider(loc)
		if err != nil {
			noFailures = false
			log.Warnf("Provider %s is unavailable in %s: %s",
				loc.provider, loc.region, err)
			continue
		}

		c.Inc(action)
		if err := fn(providerInst, providerMachines); err != nil {
			noFailures = false
			log.WithFields(log.Fields{
				"count":    len(machines),
				"action":   action,
				"provider": loc,
				"error":    err,
			}).Warn("Failed to update cloud")
		}
	}

	if noFailures {
		log.WithField("action", action).Info("Successfully updated cloud")
	} else {
		log.Infof("Due to failures, sleeping for 1 minute")
		sleep(60 * time.Second)
	}
}

type joinMachine struct {
	db.Machine

	provider db.Provider
	region   string
	role     db.Role
}

type joinResult struct {
	machines []db.Machine
	acl      db.ACL

	boot      []db.Machine
	terminate []joinMachine
	updateIPs []joinMachine
}

func (cld cloud) join() (joinResult, error) {
	res := joinResult{}

	cloudMachines, err := cld.get()
	if err != nil {
		log.WithError(err).Error("Failed to list machines")
		return res, err
	}

	err = cld.conn.Txn(db.ACLTable, db.BlueprintTable,
		db.MachineTable).Run(func(view db.Database) error {
		namespace, err := view.GetBlueprintNamespace()
		if err != nil {
			log.WithError(err).Error("Failed to get namespace")
			return err
		}

		if cld.namespace != namespace {
			err := errors.New("namespace change during a cloud run")
			log.WithError(err).Debug("Cloud run abort")
			return err
		}

		res.acl, err = view.GetACL()
		if err != nil {
			log.WithError(err).Error("Failed to get ACLs")
		}

		res.machines = view.SelectFromMachine(nil)
		cloudMachines = getMachineRoles(cloudMachines)

		dbResult := syncDB(cloudMachines, res.machines)
		res.boot = dbResult.boot
		res.terminate = dbResult.stop
		res.updateIPs = dbResult.updateIPs

		for _, pair := range dbResult.pairs {
			dbm := pair.L.(db.Machine)
			m := pair.R.(joinMachine)

			if m.role != db.None && m.role == dbm.Role {
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
		return nil
	})
	return res, err
}

func (cld cloud) syncACLs(adminACLs []string, appACLs []db.PortRange,
	machines []db.Machine) {

	// Always allow traffic from the Quilt controller.
	ip, err := myIP()
	if err == nil {
		adminACLs = append(adminACLs, ip+"/32")
	} else {
		log.WithError(err).Error("Couldn't retrieve our IP address.")
	}

	var acls []acl.ACL
	for _, adminACL := range adminACLs {
		acls = append(acls, acl.ACL{
			CidrIP:  adminACL,
			MinPort: 1,
			MaxPort: 65535,
		})
	}
	for _, appACL := range appACLs {
		acls = append(acls, acl.ACL{
			CidrIP:  "0.0.0.0/0",
			MinPort: appACL.MinPort,
			MaxPort: appACL.MaxPort,
		})
	}

	// Providers with at least one machine.
	prvdrSet := map[launchLoc]struct{}{}
	for _, m := range machines {
		if m.PublicIP != "" {
			// XXX: Look into the minimal set of necessary ports.
			acls = append(acls, acl.ACL{
				CidrIP:  m.PublicIP + "/32",
				MinPort: 1,
				MaxPort: 65535,
			})
		}
		prvdrSet[launchLoc{m.Provider, m.Region}] = struct{}{}
	}

	for loc, prvdr := range cld.providers {
		// For providers with no specified machines, we remove all ACLs.
		// Otherwise we set acls to what's specified.
		var setACLs []acl.ACL
		if _, ok := prvdrSet[loc]; ok {
			setACLs = acls
		}

		c.Inc("SetACLs")
		if err := prvdr.SetACLs(setACLs); err != nil {
			log.WithError(err).Warnf("Could not update ACLs on %s in %s.",
				loc.provider, loc.region)
		}
	}
}

type syncDBResult struct {
	pairs     []join.Pair
	boot      []db.Machine
	stop      []joinMachine
	updateIPs []joinMachine
}

func syncDB(cms []joinMachine, dbms []db.Machine) syncDBResult {
	ret := syncDBResult{}

	pair1, dbmis, cmis := join.Join(dbms, cms, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(joinMachine)

		if dbm.CloudID == m.CloudID && dbm.Provider == m.provider &&
			dbm.Preemptible == m.Preemptible &&
			dbm.Region == m.region && dbm.Size == m.Size &&
			(m.DiskSize == 0 || dbm.DiskSize == m.DiskSize) &&
			(m.role == db.None || dbm.Role == m.role) {
			return 0
		}

		return -1
	})

	pair2, dbmis, cmis := join.Join(dbmis, cmis, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(joinMachine)

		if dbm.Provider != m.provider ||
			dbm.Region != m.region ||
			dbm.Size != m.Size ||
			dbm.Preemptible != m.Preemptible ||
			(m.DiskSize != 0 && dbm.DiskSize != m.DiskSize) ||
			(m.role != db.None && dbm.Role != m.role) {
			return -1
		}

		score := 10
		if dbm.Role != db.None && m.role != db.None && dbm.Role == m.role {
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
		ret.stop = append(ret.stop, cm.(joinMachine))
	}

	for _, dbm := range dbmis {
		m := dbm.(db.Machine)
		ret.boot = append(ret.boot, m)
	}

	for _, pair := range append(pair1, pair2...) {
		dbm := pair.L.(db.Machine)
		m := pair.R.(joinMachine)

		if dbm.CloudID == m.CloudID && dbm.FloatingIP != m.FloatingIP {
			m.FloatingIP = dbm.FloatingIP
			ret.updateIPs = append(ret.updateIPs, m)
		}

		ret.pairs = append(ret.pairs, pair)
	}

	return ret
}

type listResponse struct {
	loc      launchLoc
	machines []db.Machine
	err      error
}

func (cld cloud) get() ([]joinMachine, error) {
	var wg sync.WaitGroup
	cloudMachinesChan := make(chan listResponse, len(cld.providers))
	for loc, p := range cld.providers {
		wg.Add(1)
		go func(loc launchLoc, p provider) {
			defer wg.Done()
			c.Inc("List")
			machines, err := p.List()
			cloudMachinesChan <- listResponse{loc, machines, err}
		}(loc, p)
	}
	wg.Wait()
	close(cloudMachinesChan)

	var cloudMachines []joinMachine
	for res := range cloudMachinesChan {
		if res.err != nil {
			return nil, fmt.Errorf("list %s: %s", res.loc, res.err)
		}
		for _, m := range res.machines {
			cloudMachines = append(cloudMachines, joinMachine{
				Machine:  m,
				provider: res.loc.provider,
				region:   res.loc.region,
			})
		}
	}
	return cloudMachines, nil
}

func (cld cloud) getProvider(loc launchLoc) (provider, error) {
	p, ok := cld.providers[loc]
	if ok {
		return p, nil
	}

	p, err := newProvider(loc.provider, cld.namespace, loc.region)
	if err == nil {
		cld.providers[loc] = p
	}
	return p, err
}

func getMachineRoles(machines []joinMachine) (withRoles []joinMachine) {
	for _, m := range machines {
		m.role = getMachineRole(m.PublicIP)
		withRoles = append(withRoles, m)
	}
	return withRoles
}

func groupByLoc(machines []joinMachine) map[launchLoc][]db.Machine {
	machineMap := map[launchLoc][]db.Machine{}
	for _, m := range machines {
		loc := launchLoc{m.provider, m.region}
		machineMap[loc] = append(machineMap[loc], m.Machine)
	}

	return machineMap
}

func newProviderImpl(p db.Provider, namespace, region string) (provider, error) {
	switch p {
	case db.Amazon:
		return amazon.New(namespace, region)
	case db.Google:
		return google.New(namespace, region)
	case db.DigitalOcean:
		return digitalocean.New(namespace, region)
	case db.Vagrant:
		return vagrant.New(namespace)
	default:
		panic("Unimplemented")
	}
}

func validRegionsImpl(p db.Provider) []string {
	switch p {
	case db.Amazon:
		return amazon.Regions
	case db.Google:
		return google.Zones
	case db.DigitalOcean:
		return digitalocean.Regions
	case db.Vagrant:
		return []string{""} // Vagrant has no regions
	default:
		panic("Unimplemented")
	}
}

// Stored in variables so they may be mocked out
var newProvider = newProviderImpl
var validRegions = validRegionsImpl
var getMachineRole = foreman.GetMachineRole
