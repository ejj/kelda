package cloud

import (
	"fmt"
	"time"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/amazon"
	"github.com/kelda/kelda/cloud/digitalocean"
	"github.com/kelda/kelda/cloud/foreman"
	"github.com/kelda/kelda/cloud/google"
	"github.com/kelda/kelda/cloud/vagrant"
	"github.com/kelda/kelda/connection"
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
func Run(conn db.Conn, creds connection.Credentials, adminSSHKey string) {
	foreman.Credentials = creds
	adminKey = adminSSHKey

	var ns string
	foreman.Init(conn)
	stop := make(chan struct{})
	for range conn.TriggerTick(60, db.BlueprintTable, db.MachineTable).C {
		newns, _ := conn.GetBlueprintNamespace()
		if newns == ns {
			foreman.RunOnce(conn)
			sleep(5 * time.Second) // Rate-limit the foreman.
			continue
		}

		log.Debugf("Namespace change from \"%s\", to \"%s\".", ns, newns)
		ns = newns

		if ns != "" {
			close(stop)
			stop = make(chan struct{})
			makeClouds(conn, ns, stop)
			foreman.Init(conn)
		}
	}
}

func makeClouds(conn db.Conn, ns string, stop chan struct{}) {
	for _, p := range db.AllProviders {
		for _, r := range validRegions(p) {
			cld, err := newCloud(conn, p, r, ns)
			if err != nil {
				log.WithFields(log.Fields{
					"error":  err,
					"region": cld.String(),
				}).Debug("failed to create cloud provider")
				continue
			}
			go cld.run(stop)
		}
	}
}

func newCloud(conn db.Conn, pName db.ProviderName, region, ns string) (cloud, error) {
	cld := cloud{
		conn:         conn,
		namespace:    ns,
		region:       region,
		providerName: pName,
	}

	var err error
	cld.provider, err = newProvider(pName, ns, region)
	if err != nil {
		return cld, fmt.Errorf("failed to connect: %s", err)
	}
	return cld, nil
}

func (cld cloud) run(stop <-chan struct{}) {
	log.Debugf("Start Cloud %s", cld)

	trigger := cld.conn.TriggerTick(60, db.BlueprintTable, db.MachineTable)
	defer trigger.Stop()

	for {
		select {
		case <-stop:
		case <-trigger.C:
		}

		// In a race between a closed stop and a trigger, choose stop.
		select {
		case <-stop:
			log.Debugf("Stop Cloud %s", cld)
			return
		default:
		}

		cld.runOnce()
	}
}

/* This function performs the following actions:
 * - Get the current set of machines and ACLs from the cloud provider.
 * - Get the current policy from the database.
 * - Compute a diff.
 * - Update the cloud provider accordingly.
 *
 * Updating the cloud provider may have consequences (creating machines, for example)
 * that should be reflected in the database, but won't be until `runOnce()` is called a
 * second time.  Luckily, these situations are nearly always associated with machine
 * status changes that cause a database trigger which will cause the caller to invoke
 * `runOnce()` again. */
func (cld cloud) runOnce() {
	jr, err := cloudJoin(cld)
	if err != nil {
		return
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

func (cld cloud) updateCloud(jr joinResult) {
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

	if err := util.BackoffWaitFor(pred, 30*time.Second, 5*time.Minute); err != nil {
		log.WithError(err).Warn("Failed to wait for cloud provider updates.")
	}
	log.Debug("Finished waiting for updates.")
}

func (cld cloud) syncACLs(unresolvedACLs []acl.ACL) {
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

func (cld cloud) String() string {
	return fmt.Sprintf("%s-%s-%s", cld.providerName, cld.region, cld.namespace)
}

// Stored in variables so they may be mocked out
var newProvider = newProviderImpl
var validRegions = validRegionsImpl
var getMachineRole = foreman.GetMachineRole
