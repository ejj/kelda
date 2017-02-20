package cluster

import (
	"errors"
	"time"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/amazon"
	"github.com/quilt/quilt/cluster/foreman"
	"github.com/quilt/quilt/cluster/google"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/cluster/vagrant"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
	"github.com/quilt/quilt/util"
	log "github.com/Sirupsen/logrus"
)

type provider interface {
	List() ([]machine.Machine, error)

	Boot([]machine.Machine) error

	Stop([]machine.Machine) error

	SetACLs([]acl.ACL) error

	UpdateFloatingIPs([]machine.Machine) error
}

// Store the providers in a variable so we can change it in the tests
var allProviders = []db.Provider{db.Amazon, db.Google, db.Vagrant}

type instance struct {
	provider db.Provider
	region   string
}

type cluster struct {
	namespace string
	conn      db.Conn
	providers map[instance]provider
}

var myIP = util.MyIP
var sleep = time.Sleep

// action is an enum for provider actions.
type action int

const (
	boot action = iota
	stop
	updateIPs
)

// Run continually checks 'conn' for cluster changes and recreates the cluster as
// needed.
func Run(conn db.Conn) {
	var clst *cluster
	for range conn.TriggerTick(30, db.ClusterTable, db.MachineTable, db.ACLTable).C {
		clst = updateCluster(conn, clst)

		// Somewhat of a crude rate-limit of once every five seconds to avoid
		// stressing out the cloud providers with too many API calls.
		sleep(5 * time.Second)
	}
}

func updateCluster(conn db.Conn, clst *cluster) *cluster {
	namespace, err := conn.GetClusterNamespace()
	if err != nil {
		return clst
	}

	if clst == nil || clst.namespace != namespace {
		clst = newCluster(conn, namespace)
		clst.runOnce()
		foreman.Init(clst.conn)
	}

	clst.runOnce()
	foreman.RunOnce(clst.conn)

	return clst
}

func newCluster(conn db.Conn, namespace string) *cluster {
	clst := &cluster{
		namespace: namespace,
		conn:      conn,
		providers: make(map[instance]provider),
	}

	for _, p := range allProviders {
		for _, r := range validRegions(p) {
			prvdr, err := newProvider(p, namespace, r)
			if err != nil {
				log.Debugf("Failed to connect to provider %s in %s: %s",
					p, r, err)
			} else {
				clst.providers[instance{p, r}] = prvdr
			}
		}
	}

	return clst
}

func (clst cluster) runOnce() {
	/* Each iteration of this loop does the following:
	 *
	 * - Get the current set of machines and ACLs from the cloud provider.
	 * - Get the current policy from the database.
	 * - Compute a diff.
	 * - Update the cloud provider accordingly.
	 *
	 * Updating the cloud provider may have consequences (creating machines for
	 * instances) that should be reflected in the database.  Therefore, if updates
	 * are necessary the code loops so that database can be updated before the next
	 * runOnce() call.  Once the loop as converged, it then updates the cluster ACLs
	 * before finally exiting. */
	for i := 0; i < 2; i++ {
		jr, err := clst.join()
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
			clst.syncACLs(jr.acl.Admin, jr.acl.ApplicationPorts, jr.machines)
			return
		}

		clst.updateCloud(jr.boot, boot)
		clst.updateCloud(jr.terminate, stop)
		clst.updateCloud(jr.updateIPs, updateIPs)
	}
}

func (clst cluster) updateCloud(machines []machine.Machine, act action) {
	if len(machines) == 0 {
		return
	}

	actionString := ""
	switch act {
	case boot:
		actionString = "boot"
	case stop:
		actionString = "stop"
	case updateIPs:
		actionString = "update floating IPs of"
	}

	log.WithField("count", len(machines)).
		Infof("Attempt to %s machines.", actionString)

	noFailures := true
	groupedMachines := groupBy(machines)
	for i, providerMachines := range groupedMachines {
		providerInst, ok := clst.providers[i]
		if !ok {
			noFailures = false
			log.Warnf("Provider %s is unavailable in %s.", i.provider,
				i.region)
			continue
		}
		var err error

		switch act {
		case boot:
			err = providerInst.Boot(providerMachines)
		case stop:
			err = providerInst.Stop(providerMachines)
		case updateIPs:
			err = providerInst.UpdateFloatingIPs(providerMachines)
		}

		if err != nil {
			noFailures = false
			switch act {
			case boot:
				log.WithError(err).Warnf(
					"Unable to boot machines on %s.", i.provider)
			case stop:
				log.WithError(err).Warnf(
					"Unable to stop machines on %s", i.provider)
			case updateIPs:
				log.WithError(err).Warnf(
					"Unable to update floating IPs on %s",
					i.provider)
			}
		}
	}

	if noFailures {
		switch act {
		case boot:
			log.Info("Successfully booted machines.")
		case stop:
			log.Info("Successfully stopped machines")
		case updateIPs:
			log.Info("Successfully updated floating IPs")
		}
	} else {
		log.Infof("Due to failures, sleeping for 1 minute")
		sleep(60 * time.Second)
	}
}

type joinResult struct {
	machines []db.Machine
	acl      db.ACL

	boot      []machine.Machine
	terminate []machine.Machine
	updateIPs []machine.Machine
}

func (clst cluster) join() (joinResult, error) {
	res := joinResult{}

	cloudMachines, err := clst.get()
	if err != nil {
		log.WithError(err).Error("Failed to list machines")
		return res, err
	}

	err = clst.conn.Txn(db.ACLTable, db.ClusterTable,
		db.MachineTable).Run(func(view db.Database) error {
		namespace, err := view.GetClusterNamespace()
		if err != nil {
			log.WithError(err).Error("Failed to get namespace")
			return err
		}

		if clst.namespace != namespace {
			err := errors.New("namespace change during a cluster run")
			log.WithError(err).Debug("Cluster run abort")
			return err
		}

		res.acl, err = view.GetACL()
		if err != nil {
			log.WithError(err).Error("Failed to get ACLs")
		}

		res.machines = view.SelectFromMachine(nil)

		dbResult := syncDB(cloudMachines, res.machines)
		res.boot = dbResult.boot
		res.terminate = dbResult.stop
		res.updateIPs = dbResult.updateIPs

		for _, pair := range dbResult.pairs {
			dbm := pair.L.(db.Machine)
			m := pair.R.(machine.Machine)

			dbm.CloudID = m.ID
			dbm.PublicIP = m.PublicIP
			dbm.PrivateIP = m.PrivateIP

			// We just booted the machine, can't possibly be connected.
			if dbm.PublicIP == "" {
				dbm.Connected = false
			}

			// If we overwrite the machine's size before the machine has
			// fully booted, the Stitch will flip it back immediately.
			if m.Size != "" {
				dbm.Size = m.Size
			}
			if m.DiskSize != 0 {
				dbm.DiskSize = m.DiskSize
			}
			dbm.Provider = m.Provider
			view.Commit(dbm)
		}
		return nil
	})
	return res, err
}

func (clst cluster) syncACLs(adminACLs []string, appACLs []db.PortRange,
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
	prvdrSet := map[instance]struct{}{}
	for _, m := range machines {
		if m.PublicIP != "" {
			// XXX: Look into the minimal set of necessary ports.
			acls = append(acls, acl.ACL{
				CidrIP:  m.PublicIP + "/32",
				MinPort: 1,
				MaxPort: 65535,
			})
		}
		prvdrSet[instance{m.Provider, m.Region}] = struct{}{}
	}

	for inst, prvdr := range clst.providers {
		// For providers with no specified machines, we remove all ACLs.
		// Otherwise we set acls to what's specified.
		var setACLs []acl.ACL
		if _, ok := prvdrSet[inst]; ok {
			setACLs = acls
		}

		if err := prvdr.SetACLs(setACLs); err != nil {
			log.WithError(err).Warnf("Could not update ACLs on %s in %s.",
				inst.provider, inst.region)
		}
	}
}

type syncDBResult struct {
	pairs     []join.Pair
	boot      []machine.Machine
	stop      []machine.Machine
	updateIPs []machine.Machine
}

func syncDB(cms []machine.Machine, dbms []db.Machine) syncDBResult {
	ret := syncDBResult{}

	pair1, dbmis, cmis := join.Join(dbms, cms, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(machine.Machine)

		if dbm.CloudID == m.ID && dbm.Provider == m.Provider &&
			dbm.Region == m.Region && dbm.Size == m.Size &&
			(m.DiskSize == 0 || dbm.DiskSize == m.DiskSize) {
			return 0
		}

		return -1
	})

	pair2, dbmis, cmis := join.Join(dbmis, cmis, func(l, r interface{}) int {
		dbm := l.(db.Machine)
		m := r.(machine.Machine)

		switch {
		case dbm.Provider != m.Provider ||
			dbm.Region != m.Region ||
			dbm.Size != m.Size ||
			(m.DiskSize != 0 && dbm.DiskSize != m.DiskSize):
			return -1
		case dbm.CloudID == m.ID:
			panic("Not Reached") // Should have been hit by the first join.
		case dbm.FloatingIP == m.FloatingIP:
			return 1
		case dbm.PublicIP == m.PublicIP || dbm.PrivateIP == m.PrivateIP:
			return 2
		default:
			return 3
		}
	})

	for _, cm := range cmis {
		ret.stop = append(ret.stop, cm.(machine.Machine))
	}

	for _, dbm := range dbmis {
		m := dbm.(db.Machine)
		ret.boot = append(ret.boot, machine.Machine{
			Size:     m.Size,
			Provider: m.Provider,
			Region:   m.Region,
			DiskSize: m.DiskSize,
			SSHKeys:  m.SSHKeys})
	}

	for _, pair := range append(pair1, pair2...) {
		dbm := pair.L.(db.Machine)
		m := pair.R.(machine.Machine)

		if dbm.FloatingIP != m.FloatingIP {
			m.FloatingIP = dbm.FloatingIP
			ret.updateIPs = append(ret.updateIPs, m)
		}

		ret.pairs = append(ret.pairs, pair)
	}

	return ret
}

func (clst cluster) get() ([]machine.Machine, error) {
	var cloudMachines []machine.Machine
	for _, p := range clst.providers {
		providerMachines, err := p.List()
		if err != nil {
			return []machine.Machine{}, err
		}
		cloudMachines = append(cloudMachines, providerMachines...)
	}
	return cloudMachines, nil
}

func groupBy(machines []machine.Machine) map[instance][]machine.Machine {
	machineMap := map[instance][]machine.Machine{}
	for _, m := range machines {
		i := instance{m.Provider, m.Region}
		machineMap[i] = append(machineMap[i], m)
	}

	return machineMap
}

func newProviderImpl(p db.Provider, namespace, region string) (provider, error) {
	switch p {
	case db.Amazon:
		return amazon.New(namespace, region)
	case db.Google:
		return google.New(namespace, region)
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
	case db.Vagrant:
		return []string{""} // Vagrant has no regions
	default:
		panic("Unimplemented")
	}
}

func groupByRegion(machines []machine.Machine) map[string][]machine.Machine {
	groups := map[string][]machine.Machine{}
	for _, m := range machines {
		groups[m.Region] = append(groups[m.Region], m)
	}

	return groups
}

// Stored in variables so they may be mocked out
var newProvider = newProviderImpl
var validRegions = validRegionsImpl
