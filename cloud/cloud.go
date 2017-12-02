package cloud

import (
	"fmt"
	"time"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/amazon"
	"github.com/kelda/kelda/cloud/digitalocean"
	"github.com/kelda/kelda/cloud/foreman"
	"github.com/kelda/kelda/cloud/google"
	"github.com/kelda/kelda/cloud/vagrant"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
	log "github.com/sirupsen/logrus"
)

type provider interface {
	List() ([]db.Machine, error)

	// Takes a set of db.Machines, and returns the CloudIDs the machines will have
	// once they boot.
	Boot([]db.Machine) ([]string, error)

	Stop([]db.Machine) error

	SetACLs([]acl.ACL) error

	UpdateFloatingIPs([]db.Machine) error

	// The Cleanup() function will be called occaisionally in those regions that have
	// no machines running, and no machines expected to be running in the future.
	// The provider may use this method to free up resources that are only necessary
	// when machines are running.  Note that a call to Cleanup() does not guarnatee
	// that at some point in the future calls to the other provider methods won't
	// occur, so nothing should be done in this method that can't be undone later if
	// necessary.
	Cleanup() error
}

var c = counter.New("Cloud")

type cloud struct {
	conn db.Conn

	namespace    string
	providerName db.ProviderName
	region       string
	provider     provider
}

var myIP = util.MyIP
var sleep = time.Sleep
var adminKey string

const defaultDiskSize = 32

// Run continually checks 'conn' for cloud changes and recreates the cloud as
// needed.
func Run(conn db.Conn, adminSSHKey string) {
	adminKey = adminSSHKey

	var ns string
	stop := make(chan struct{})
	for range conn.TriggerTick(60, db.BlueprintTable, db.MachineTable).C {
		newns, _ := conn.GetBlueprintNamespace()
		if newns == ns {
			continue
		}

		log.Debugf("Namespace change from \"%s\", to \"%s\".", ns, newns)
		ns = newns

		if ns != "" {
			close(stop)
			stop = make(chan struct{})
			startClouds(conn, ns, stop)
		}
	}
}

func startClouds(conn db.Conn, ns string, stop chan struct{}) {
	for _, p := range db.AllProviders {
		for _, r := range validRegions(p) {
			go func(p db.ProviderName, r string) {
				cld := cloud{
					conn:         conn,
					namespace:    ns,
					region:       r,
					providerName: p,
				}
				cld.run(stop)
			}(p, r)
		}
	}
}

func (cld *cloud) run(stop <-chan struct{}) {
	log.Debugf("Start Cloud %s", cld)

	// Note that we need to be fairly conservative about running due to request
	// limits enforced by the cloud providers.  On unused regions we run very rarely
	// as a result.
	fast := time.NewTicker(5 * time.Second)
	slow := time.NewTicker(2 * time.Minute)
	defer fast.Stop()
	defer slow.Stop()

	trigger := cld.conn.Trigger(db.BlueprintTable, db.MachineTable)
	defer trigger.Stop()

	// This loop executes runOnce() whenever the database triggers, or a tick fires.
	// We choose a fast tick in those regions that have machines, and a slow tick in
	// those regions that are empty.  In the event the stop channel is closed, the
	// function returns.
	for {
		tick := slow.C
		if cld.runOnce() {
			tick = fast.C
		}

		// If we switched tickers, there may be a stale tick to drain.  If
		// there's no tick, fall through immediately.
		select {
		case <-tick:
		default:
		}

		select {
		case <-stop:
		case <-trigger.C:
		case <-tick:
		}

		// In a race between a closed stop and a trigger, choose stop.
		select {
		case <-stop:
			log.Debugf("Stop Cloud %s", cld)
			return
		default:
		}
	}
}

/* This function performs the following actions:
 * - Initialize the connection to the cloud provider (if it hasn't been initialized
 *   already).
 * - Get the current set of machines and ACLs from the cloud provider.
 * - Get the current policy from the database.
 * - Compute a diff.
 * - Update the cloud provider accordingly.
 *
 * Updating the cloud provider may have consequences (creating machines, for example)
 * that should be reflected in the database, but won't be until `runOnce()` is called a
 * second time.  Luckily, these situations are nearly always associated with machine
 * status changes that cause a database trigger which will cause the caller to invoke
 * `runOnce()` again.
 *
 * `runOnce()` returns a hint as to whether or not this region is `active` (i.e. it has
 * machines).  Callers may want to call `runOnce()` more frequently on `active` regions
 * than they would run empty ones. */
func (cld *cloud) runOnce() (active bool) {
	// If the provider is not initialized, try to do so. We do this here
	// so that we keep trying when there's an error.
	if cld.provider == nil {
		provider, err := newProvider(cld.providerName, cld.namespace, cld.region)
		if err != nil {
			logger := log.WithFields(log.Fields{
				"provider": cld.String(),
				"error":    err,
			})
			message := "failed to initialize cloud provider %s(will keep " +
				"retrying)"
			if cld.usedByCurrentBlueprint() {
				logger.Errorf(message, "used by the current blueprint ")
				return true
			}
			logger.Debugf(message, "")
			return false
		}
		cld.provider = provider
	}

	jr, err := cloudJoin(cld)
	if err != nil {
		// Could have failed due to a misconfiguration (bad keys, network
		// connectivity issues, insufficient permissions, etc.). In that case
		// there's no particular advantage to checking regularly, so we consider
		// it inactive.
		return false
	}

	if !jr.isActive {
		if err := cld.provider.Cleanup(); err != nil {
			log.WithError(err).WithField("region", cld.String()).Debug(
				"Failed to clean up region")
		}
		return jr.isActive
	}

	if len(jr.boot) == 0 &&
		len(jr.terminate) == 0 &&
		len(jr.updateIPs) == 0 {
		// ACLs must be processed after Kelda learns about what machines
		// are in the cloud.  If we didn't, inter-machine ACLs could get
		// removed when the Kelda controller restarts, even if there are
		// running cloud machines that still need to communicate.
		cld.syncACLs(jr.acls)
	} else {
		cld.updateCloud(jr)
	}

	return jr.isActive
}

// usedByCurrentBlueprint returns whether this cloud provider is used by machines
// in the blueprint that is currently active.
func (cld *cloud) usedByCurrentBlueprint() bool {
	var bp db.Blueprint
	var err error
	cld.conn.Txn(db.BlueprintTable).Run(
		func(view db.Database) error {
			bp, err = view.GetBlueprint()
			return nil
		})
	if err != nil {
		log.WithFields(log.Fields{
			"provider": cld.String(),
			"error":    err,
		}).Warn("failed to determine whether cloud provider " +
			"is used by the current blueprint")
		// If we're not sure if the cloud provider is used,
		// conservatively assume that it is.
		return true
	}
	return len(cld.desiredMachines(bp.Blueprint.Machines)) > 0
}

// desiredMachines takes a list of all machines specified by a blueprint, and returns
// a list of database machines that includes only the machines for this cloud's
// provider and region.
func (cld *cloud) desiredMachines(bpms []blueprint.Machine) []db.Machine {
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

func sanitizeMachines(machines []db.Machine) []db.Machine {
	// As a defensive measure, we only copy over the fields that the underlying
	// provider should care about instead of passing `machines` to updateCloud
	// directly.
	var cloudMachines []db.Machine
	for _, m := range machines {
		cloudMachines = append(cloudMachines, db.Machine{
			CloudID:     m.CloudID,
			Size:        m.Size,
			DiskSize:    m.DiskSize,
			Preemptible: m.Preemptible,
			SSHKeys:     m.SSHKeys,
			Role:        m.Role,
			Provider:    m.Provider,
			Region:      m.Region,
			FloatingIP:  m.FloatingIP,
		})
	}
	return cloudMachines
}

func (cld *cloud) updateCloud(jr joinResult) {
	logAttempt := func(count int, action string, err error) {
		c.Inc(action)
		logFields := log.Fields{
			"count":  count,
			"action": action,
			"region": cld.String()}
		if err != nil {
			logFields["error"] = err
			log.WithFields(logFields).Error(
				"Failed to update cloud provider.")
		} else {
			log.WithFields(logFields).Infof("Cloud provider update.")
		}
	}

	var bootIDs []string
	if len(jr.boot) > 0 {
		var err error
		bootIDs, err = cld.provider.Boot(sanitizeMachines(jr.boot))
		logAttempt(len(jr.boot), "boot", err)
	}

	if len(jr.terminate) > 0 {
		err := cld.provider.Stop(sanitizeMachines(jr.terminate))
		logAttempt(len(jr.terminate), "stop", err)
		if err != nil {
			jr.terminate = nil // Don't wait if we errored.
		}
	}

	if len(jr.updateIPs) > 0 {
		err := cld.provider.UpdateFloatingIPs(sanitizeMachines(jr.updateIPs))
		logAttempt(len(jr.updateIPs), "update floating IPs", err)
		if err != nil {
			jr.updateIPs = nil // Don't wait if we errored.
		}
	}

	pred := func() bool {
		machines, err := cld.provider.List()
		if err != nil {
			log.WithError(err).Warn("Failed to list machines.")
			return true
		}

		ids := map[string]db.Machine{}
		for _, m := range machines {
			ids[m.CloudID] = m
		}

		for _, id := range bootIDs {
			if _, ok := ids[id]; !ok {
				return false
			}
		}

		for _, m := range jr.terminate {
			if _, ok := ids[m.CloudID]; ok {
				return false
			}
		}

		for _, jrm := range jr.updateIPs {
			m, ok := ids[jrm.CloudID]
			if ok && m.FloatingIP != jrm.FloatingIP {
				return false
			}
		}

		return true
	}

	if err := util.BackoffWaitFor(pred, 10*time.Second, 5*time.Minute); err != nil {
		log.WithError(err).Warn("Failed to wait for cloud provider updates.")
	}
	log.Debug("Finished waiting for updates.")
}

func (cld *cloud) syncACLs(unresolvedACLs []acl.ACL) {
	var acls []acl.ACL
	for _, acl := range unresolvedACLs {
		if acl.CidrIP == "local" {
			ip, err := myIP()
			if err != nil {
				log.WithError(err).Error("Failed to retrive local IP.")
				return
			}
			acl.CidrIP = ip + "/32"
		}
		acls = append(acls, acl)
	}

	c.Inc("SetACLs")
	if err := cld.provider.SetACLs(acls); err != nil {
		log.WithError(err).Warnf("Could not update ACLs in %s.", cld)
	}
}

func newProviderImpl(p db.ProviderName, namespace, region string) (provider, error) {
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

func validRegionsImpl(p db.ProviderName) []string {
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

func (cld *cloud) String() string {
	return fmt.Sprintf("%s-%s-%s", cld.providerName, cld.region, cld.namespace)
}

// Stored in variables so they may be mocked out
var newProvider = newProviderImpl
var validRegions = validRegionsImpl
var getMachineRole = foreman.GetMachineRole
