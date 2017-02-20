package google

////// SET UP API ACCESS:
//
// 1) In the Google Developer Console navigate to:
//    Permissions > Service accounts
//
// 2) Create or use an existing Service Account
//
// 3) For your Service Account, create and save a key as "~/.gce/quilt.json"
//
// 4) In the Google Developer Console navigate to:
//    Permissions > Permissions
//
// 5) If the Service Account is not already, assign it the "Editor" role.
//    You select the account by email.

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/cloudcfg"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"

	log "github.com/Sirupsen/logrus"
	"github.com/satori/go.uuid"
	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
)

// DefaultRegion is the preferred location for machines which haven't a user specified
// region preference.
const DefaultRegion = "us-east1-b"

// Zones is the list of supported GCE zones
var Zones = []string{"us-central1-a", "us-east1-b", "europe-west1-b"}

// ephemeralIPName is a constant for what we label NATs with ephemeral IPs in GCE.
const ephemeralIPName = "External NAT"

// floatingIPName is a constant for what we label NATs with floating IPs in GCE.
const floatingIPName = "Floating IP"

const computeBaseURL string = "https://www.googleapis.com/compute/v1/projects"
const (
	// These are the various types of Operations that the GCE API returns
	local = iota
	global
)

// The Cluster objects represents a connection to GCE.
type Cluster struct {
	gce client

	projID    string // gce project ID
	imgURL    string // gce url to the VM image
	baseURL   string // gce project specific url prefix
	ipv4Range string // ipv4 range of the internal network
	intFW     string // gce internal firewall name
	zone      string // gce boot region

	ns string // cluster namespace
	id int    // the id of the cluster, used externally
}

// New creates a GCE cluster.
//
// Clusters are differentiated (namespace) by setting the description and
// filtering off of that.
func New(namespace, zone string) (*Cluster, error) {
	gce, err := newClient()
	if err != nil {
		log.WithError(err).Error("Failed to initialize GCE client")
		return nil, err
	}

	clst := Cluster{
		gce:       gce,
		projID:    "declarative-infrastructure",
		ns:        namespace,
		ipv4Range: "192.168.0.0/16",
		zone:      zone,
	}
	clst.baseURL = fmt.Sprintf("%s/%s", computeBaseURL, clst.projID)
	clst.intFW = fmt.Sprintf("%s-internal", clst.ns)
	clst.imgURL = fmt.Sprintf("%s/%s", computeBaseURL,
		"ubuntu-os-cloud/global/images/ubuntu-1604-xenial-v20160921")

	if err := clst.netInit(); err != nil {
		log.WithError(err).Debug("failed to start up gce network")
		return nil, err
	}

	if err := clst.fwInit(); err != nil {
		log.WithError(err).Debug("failed to start up gce firewalls")
		return nil, err
	}

	return &clst, nil
}

// List the current machines in the cluster.
func (clst *Cluster) List() ([]machine.Machine, error) {
	// XXX: This doesn't use the instance group listing functionality because
	// listing that way doesn't get you information about the instances
	var mList []machine.Machine
	list, err := clst.gce.ListInstances(clst.projID, clst.zone, apiOptions{
		filter: fmt.Sprintf("description eq %s", clst.ns),
	})
	if err != nil {
		return nil, err
	}
	for _, item := range list.Items {
		// XXX: This make some iffy assumptions about NetworkInterfaces
		machineSplitURL := strings.Split(item.MachineType, "/")
		mtype := machineSplitURL[len(machineSplitURL)-1]

		accessConfig := item.NetworkInterfaces[0].AccessConfigs[0]
		floatingIP := ""
		if accessConfig.Name == floatingIPName {
			floatingIP = accessConfig.NatIP
		}

		mList = append(mList, machine.Machine{
			ID:         item.Name,
			PublicIP:   accessConfig.NatIP,
			FloatingIP: floatingIP,
			PrivateIP:  item.NetworkInterfaces[0].NetworkIP,
			Size:       mtype,
			Region:     clst.zone,
			Provider:   db.Google,
		})
	}
	return mList, nil
}

// Boot blocks while creating instances.
func (clst *Cluster) Boot(bootSet []machine.Machine) error {
	// XXX: should probably have a better clean up routine if an error is encountered
	var names []string
	for _, m := range bootSet {
		name := "quilt-" + uuid.NewV4().String()
		_, err := clst.instanceNew(name, m.Size,
			cloudcfg.Ubuntu(m.SSHKeys, "xenial"))
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"id":    m.ID,
			}).Error("Failed to start instance.")
			continue
		}
		names = append(names, name)
	}
	if err := clst.wait(names, true); err != nil {
		return err
	}
	return nil
}

// Stop blocks while deleting the instances.
//
// If an error occurs while deleting, it will finish the ones that have
// successfully started before returning.
func (clst *Cluster) Stop(machines []machine.Machine) error {
	// XXX: should probably have a better clean up routine if an error is encountered
	var names []string
	for _, m := range machines {
		_, err := clst.gce.DeleteInstance(clst.projID, m.Region, m.ID)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"id":    m.ID,
			}).Error("Failed to delete instance.")
			continue
		}
		names = append(names, m.ID)
	}
	if err := clst.wait(names, false); err != nil {
		return err
	}
	return nil
}

// Get() and operationWait() don't always present the same results, so
// Boot() and Stop() must have a special wait to stay in sync with Get().
func (clst *Cluster) wait(names []string, live bool) error {
	if len(names) == 0 {
		return nil
	}

	after := time.After(3 * time.Minute)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()

	for range tick.C {
		select {
		case <-after:
			return errors.New("wait(): timeout")
		default:
		}

		for len(names) > 0 {
			name := names[0]
			instances, err := clst.List()
			if err != nil {
				return err
			}
			exists := false
			for _, ist := range instances {
				if name == ist.ID {
					exists = true
				}
			}
			if live == exists {
				names = append(names[:0], names[1:]...)
			}
		}
		if len(names) == 0 {
			return nil
		}
	}
	return nil
}

// Blocking wait with a hardcoded timeout.
//
// Waits on operations, the type of which is indicated by 'domain'. All
// operations must be of the same 'domain'
//
// XXX: maybe not hardcode timeout, and retry interval
func (clst *Cluster) operationWait(ops []*compute.Operation, domain int) error {
	if len(ops) == 0 {
		return nil
	}

	after := time.After(3 * time.Minute)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()

	var op *compute.Operation
	var err error
	for {
		select {
		case <-after:
			return fmt.Errorf("operationWait(): timeout")
		case <-tick.C:
			for len(ops) > 0 {
				switch {
				case domain == local:
					op, err = clst.gce.GetZoneOperation(clst.projID,
						ops[0].Zone, ops[0].Name)
				case domain == global:
					op, err = clst.gce.GetGlobalOperation(clst.projID,
						ops[0].Name)
				}
				if err != nil {
					return err
				}
				if op.Status != "DONE" {
					break
				}
				ops = append(ops[:0], ops[1:]...)
			}
			if len(ops) == 0 {
				return nil
			}
		}
	}
}

// Create new GCE instance.
//
// Does not check if the operation succeeds.
//
// XXX: all kinds of hardcoded junk in here
// XXX: currently only defines the bare minimum
func (clst *Cluster) instanceNew(name string, size string,
	cloudConfig string) (*compute.Operation, error) {
	instance := &compute.Instance{
		Name:        name,
		Description: clst.ns,
		MachineType: fmt.Sprintf("%s/zones/%s/machineTypes/%s",
			clst.baseURL,
			clst.zone,
			size),
		Disks: []*compute.AttachedDisk{
			{
				Boot:       true,
				AutoDelete: true,
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: clst.imgURL,
				},
			},
		},
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				AccessConfigs: []*compute.AccessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: ephemeralIPName,
					},
				},
				Network: fmt.Sprintf("%s/global/networks/%s",
					clst.baseURL,
					clst.ns),
			},
		},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{
				{
					Key:   "startup-script",
					Value: &cloudConfig,
				},
			},
		},
	}

	return clst.gce.InsertInstance(clst.projID, clst.zone, instance)
}

func (clst *Cluster) parseACLs(fws []*compute.Firewall) (acls []acl.ACL) {
	for _, fw := range fws {
		if fw.Name == clst.intFW {
			continue
		}
		for _, cidrIP := range fw.SourceRanges {
			for _, allowed := range fw.Allowed {
				for _, portsStr := range allowed.Ports {
					for _, ports := range strings.Split(
						portsStr, ",") {

						portRange := strings.Split(ports, "-")
						var minPort, maxPort int
						switch len(portRange) {
						case 0:
							minPort, maxPort = 1, 65535
						case 1:
							port, _ := strconv.Atoi(
								portRange[0])
							minPort, maxPort = port, port
						default:
							minPort, _ = strconv.Atoi(
								portRange[0])
							maxPort, _ = strconv.Atoi(
								portRange[1])
						}
						acls = append(acls, acl.ACL{
							CidrIP:  cidrIP,
							MinPort: minPort,
							MaxPort: maxPort,
						})
					}
				}
			}
		}
	}

	return acls
}

// SetACLs adds and removes acls in `clst` so that it conforms to `acls`.
func (clst *Cluster) SetACLs(acls []acl.ACL) error {
	list, err := clst.gce.ListFirewalls(clst.projID)
	if err != nil {
		return err
	}

	currACLs := clst.parseACLs(list.Items)
	pair, toAdd, toRemove := join.HashJoin(acl.Slice(acls), acl.Slice(currACLs),
		nil, nil)

	var toSet []acl.ACL
	for _, a := range toAdd {
		toSet = append(toSet, a.(acl.ACL))
	}
	for _, p := range pair {
		toSet = append(toSet, p.L.(acl.ACL))
	}
	for _, a := range toRemove {
		toSet = append(toSet, acl.ACL{
			MinPort: a.(acl.ACL).MinPort,
			MaxPort: a.(acl.ACL).MaxPort,
			CidrIP:  "", // Remove all currently allowed IPs.
		})
	}

	for acl, cidrIPs := range groupACLsByPorts(toSet) {
		fw, err := clst.getCreateFirewall(acl.MinPort, acl.MaxPort)
		if err != nil {
			return err
		}

		if reflect.DeepEqual(fw.SourceRanges, cidrIPs) {
			continue
		}

		var op *compute.Operation
		if len(cidrIPs) == 0 {
			log.WithField("ports", fmt.Sprintf(
				"%d-%d", acl.MinPort, acl.MaxPort)).
				Debug("Google: Deleting firewall")
			op, err = clst.gce.DeleteFirewall(clst.projID, fw.Name)
			if err != nil {
				return err
			}
		} else {
			log.WithField("ports", fmt.Sprintf(
				"%d-%d", acl.MinPort, acl.MaxPort)).
				WithField("CidrIPs", cidrIPs).
				Debug("Google: Setting ACLs")
			op, err = clst.firewallPatch(fw.Name, cidrIPs)
			if err != nil {
				return err
			}
		}
		if err := clst.operationWait(
			[]*compute.Operation{op}, global); err != nil {
			return err
		}
	}

	return nil
}

// UpdateFloatingIPs updates IPs of machines by recreating their network interfaces.
func (clst *Cluster) UpdateFloatingIPs(machines []machine.Machine) error {
	for _, m := range machines {
		instance, err := clst.gce.GetInstance(clst.projID, m.Region, m.ID)
		if err != nil {
			return err
		}

		// Delete existing network interface. It is only possible to assign
		// one access config per instance. Thus, updating GCE Floating IPs
		// is not a seamless, zero-downtime procedure.
		networkInterface := instance.NetworkInterfaces[0]
		accessConfig := instance.NetworkInterfaces[0].AccessConfigs[0]
		_, err = clst.gce.DeleteAccessConfig(clst.projID, m.Region, m.ID,
			accessConfig.Name, networkInterface.Name)
		if err != nil {
			return err
		}

		// Add new network interface.
		_, err = clst.gce.AddAccessConfig(clst.projID, m.Region, m.ID,
			networkInterface.Name, &compute.AccessConfig{
				Type: "ONE_TO_ONE_NAT",
				Name: floatingIPName,
				// Google will automatically assign a dynamic IP
				// if none is provided (i.e. m.FloatingIP == "").
				NatIP: m.FloatingIP,
			})
		if err != nil {
			return err
		}
	}

	return nil
}

func (clst *Cluster) getFirewall(name string) (*compute.Firewall, error) {
	list, err := clst.gce.ListFirewalls(clst.projID)
	if err != nil {
		return nil, err
	}
	for _, val := range list.Items {
		if val.Name == name {
			return val, nil
		}
	}

	return nil, nil
}

func (clst *Cluster) getCreateFirewall(minPort int, maxPort int) (
	*compute.Firewall, error) {

	ports := fmt.Sprintf("%d-%d", minPort, maxPort)
	fwName := fmt.Sprintf("%s-%s", clst.ns, ports)

	if fw, _ := clst.getFirewall(fwName); fw != nil {
		return fw, nil
	}

	log.WithField("name", fwName).Debug("Creating firewall")
	op, err := clst.insertFirewall(fwName, ports, []string{"127.0.0.1/32"})
	if err != nil {
		return nil, err
	}

	if err := clst.operationWait([]*compute.Operation{op}, global); err != nil {
		return nil, err
	}

	return clst.getFirewall(fwName)
}

func (clst *Cluster) networkExists(name string) (bool, error) {
	list, err := clst.gce.ListNetworks(clst.projID)
	if err != nil {
		return false, err
	}
	for _, val := range list.Items {
		if val.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// This creates a firewall but does nothing else
//
// XXX: Assumes there is only one network
func (clst *Cluster) insertFirewall(name, ports string, sourceRanges []string) (
	*compute.Operation, error) {
	firewall := &compute.Firewall{
		Name: name,
		Network: fmt.Sprintf("%s/global/networks/%s",
			clst.baseURL,
			clst.ns),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
				Ports:      []string{ports},
			},
			{
				IPProtocol: "udp",
				Ports:      []string{ports},
			},
			{
				IPProtocol: "icmp",
			},
		},
		SourceRanges: sourceRanges,
	}

	return clst.gce.InsertFirewall(clst.projID, firewall)
}

func (clst *Cluster) firewallExists(name string) (bool, error) {
	fw, err := clst.getFirewall(name)
	return fw != nil, err
}

// Updates the firewall using PATCH semantics.
//
// The IP addresses must be in CIDR notation.
// XXX: Assumes there is only one network
// XXX: Assumes the firewall only needs to adjust the IP addrs affected
func (clst *Cluster) firewallPatch(name string,
	ips []string) (*compute.Operation, error) {
	firewall := &compute.Firewall{
		Name: name,
		Network: fmt.Sprintf("%s/global/networks/%s",
			clst.baseURL,
			clst.ns),
		SourceRanges: ips,
	}

	return clst.gce.PatchFirewall(clst.projID, name, firewall)
}

func newComputeService(ctx context.Context) (*compute.Service, error) {
	client, err := google.DefaultClient(ctx, compute.ComputeScope)
	if err != nil {
		return nil, err
	}
	computeService, err := compute.New(client)
	if err != nil {
		return nil, err
	}
	return computeService, nil
}

// Initializes the network for the cluster
//
// XXX: Currently assumes that each cluster is entirely behind 1 network
func (clst *Cluster) netInit() error {
	exists, err := clst.networkExists(clst.ns)
	if err != nil {
		return err
	}

	if exists {
		log.Debug("Network already exists")
		return nil
	}

	log.Debug("Creating network")
	op, err := clst.gce.InsertNetwork(clst.projID, &compute.Network{
		Name:      clst.ns,
		IPv4Range: clst.ipv4Range,
	})
	if err != nil {
		return err
	}

	err = clst.operationWait([]*compute.Operation{op}, global)
	if err != nil {
		return err
	}
	return nil
}

// Initializes the firewall for the cluster
//
// XXX: Currently assumes that each cluster is entirely behind 1 network
func (clst *Cluster) fwInit() error {
	var ops []*compute.Operation

	if exists, err := clst.firewallExists(clst.intFW); err != nil {
		return err
	} else if exists {
		log.Debug("internal firewall already exists")
	} else {
		log.Debug("creating internal firewall")
		op, err := clst.insertFirewall(
			clst.intFW, "1-65535", []string{clst.ipv4Range})
		if err != nil {
			return err
		}
		ops = append(ops, op)
	}

	if err := clst.operationWait(ops, global); err != nil {
		return err
	}
	return nil
}

func groupACLsByPorts(acls []acl.ACL) map[acl.ACL][]string {
	grouped := make(map[acl.ACL][]string)
	for _, a := range acls {
		key := acl.ACL{
			MinPort: a.MinPort,
			MaxPort: a.MaxPort,
		}
		if _, ok := grouped[key]; !ok {
			grouped[key] = nil
		}
		if a.CidrIP != "" {
			grouped[key] = append(grouped[key], a.CidrIP)
		}
	}
	return grouped
}
