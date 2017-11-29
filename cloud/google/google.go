package google

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/google/client"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
	compute "google.golang.org/api/compute/v1"
)

// DefaultRegion is the preferred location for machines that don't have a
// user specified region preference.
const DefaultRegion = "us-east1-b"

// Zones is the list of supported GCE zones
var Zones = []string{"us-central1-a", "us-east1-b", "europe-west1-b"}

// ephemeralIPName is a constant for what we label NATs with ephemeral IPs in GCE.
const ephemeralIPName = "External NAT"

// floatingIPName is a constant for what we label NATs with floating IPs in GCE.
const floatingIPName = "Floating IP"

const image = "https://www.googleapis.com/compute/v1/projects/" +
	"ubuntu-os-cloud/global/images/ubuntu-1604-xenial-v20170202"

const ipv4Range string = "172.16.0.0/12"

// The Provider objects represents a connection to GCE.
type Provider struct {
	client.Client

	namespace string // client namespace
	network   string // gce identifier for the network
	zone      string // gce boot region
}

// New creates a GCE client.
//
// Providers are differentiated (namespace) by setting the description and
// filtering off of that.
func New(namespace, zone string) (*Provider, error) {
	gce, err := client.New()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCE client: %s", err.Error())
	}

	prvdr := Provider{
		Client:    gce,
		namespace: namespace,
		network:   fmt.Sprintf("kelda-%s-%s", namespace, zone),
		zone:      zone,
	}

	return &prvdr, nil
}

// getNetworkConfig extracts the NetworkInterface and AccessConfig from a
// Google instance, and handles checking that these are properly
// defined in the instance.
func getNetworkConfig(inst *compute.Instance) (
	*compute.NetworkInterface, *compute.AccessConfig, error) {
	if len(inst.NetworkInterfaces) != 1 {
		return nil, nil, fmt.Errorf("Google instances are expected to "+
			"have exactly 1 interface; for instance %s, "+
			"found %d", inst.Name, len(inst.NetworkInterfaces))
	}
	iface := inst.NetworkInterfaces[0]
	if len(iface.AccessConfigs) != 1 {
		return nil, nil, fmt.Errorf("Google instances expected to "+
			"have exactly 1 access config (instances "+
			"without a config will not be accessible "+
			"via the public internet, and Google "+
			"does not support more than one config); "+
			"for instance %s, found %d access configs",
			inst.Name, len(iface.AccessConfigs))
	}
	return iface, iface.AccessConfigs[0], nil
}

// List the current machines in the cluster.
func (prvdr *Provider) List() ([]db.Machine, error) {
	var machines []db.Machine
	instances, err := prvdr.ListInstances(prvdr.zone, prvdr.network)
	if err != nil {
		return nil, err
	}
	for _, instance := range instances.Items {
		machineSplitURL := strings.Split(instance.MachineType, "/")
		mtype := machineSplitURL[len(machineSplitURL)-1]

		iface, accessConfig, err := getNetworkConfig(instance)
		if err != nil {
			return nil, err
		}

		floatingIP := ""
		if accessConfig.Name == floatingIPName {
			floatingIP = accessConfig.NatIP
		}

		machines = append(machines, db.Machine{
			Provider:   db.Google,
			Region:     prvdr.zone,
			CloudID:    instance.Name,
			PublicIP:   accessConfig.NatIP,
			FloatingIP: floatingIP,
			PrivateIP:  iface.NetworkIP,
			Size:       mtype,
		})
	}
	return machines, nil
}

// Boot blocks while creating instances.
func (prvdr *Provider) Boot(bootSet []db.Machine) ([]string, error) {
	if err := prvdr.createNetwork(); err != nil {
		return nil, err
	}

	var names []string
	errChan := make(chan error)

	for _, m := range bootSet {
		if m.Preemptible {
			return nil, errors.New("preemptible vms are not implemented")
		}

		name := randName()
		names = append(names, name)

		go func(m db.Machine) {
			icfg := prvdr.instanceConfig(name, m.Size, cfg.Ubuntu(m, ""))
			_, err := prvdr.InsertInstance(prvdr.zone, icfg)
			errChan <- err
		}(m)
	}

	for i := 0; i < len(bootSet); i++ {
		if err := <-errChan; err != nil {
			return nil, err
		}
	}

	return names, nil
}

// Stop blocks while deleting the instances.
//
// If an error occurs while deleting, it will finish the ones that have
// successfully started before returning.
func (prvdr *Provider) Stop(machines []db.Machine) error {
	errChan := make(chan error)
	for _, m := range machines {
		go func(m db.Machine) {
			_, err := prvdr.DeleteInstance(prvdr.zone, m.CloudID)
			errChan <- err
		}(m)
	}

	for i := 0; i < len(machines); i++ {
		if err := <-errChan; err != nil {
			return err
		}
	}
	return nil
}

// Blocking wait with a hardcoded timeout.
//
// Waits for the given operations to all complete.
var backoffWaitFor = util.BackoffWaitFor

func (prvdr *Provider) operationWait(ops ...*compute.Operation) (err error) {
	return backoffWaitFor(func() bool {
		for _, op := range ops {
			var res *compute.Operation
			if op.Zone != "" {
				res, err = prvdr.GetZoneOperation(
					path.Base(op.Zone), op.Name)
			} else {
				res, err = prvdr.GetGlobalOperation(op.Name)
			}

			if err != nil || res.Status != "DONE" {
				return false
			}
		}
		return true
	}, 10*time.Second, 3*time.Minute)
}

func (prvdr Provider) instanceConfig(name, size, cloudConfig string) *compute.Instance {
	return &compute.Instance{
		Name:        name,
		Description: prvdr.network,
		MachineType: fmt.Sprintf("zones/%s/machineTypes/%s", prvdr.zone, size),
		Disks: []*compute.AttachedDisk{{
			Boot:       true,
			AutoDelete: true,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				SourceImage: image,
			},
		}},
		NetworkInterfaces: []*compute.NetworkInterface{{
			AccessConfigs: []*compute.AccessConfig{{
				Type: "ONE_TO_ONE_NAT",
				Name: ephemeralIPName,
			}},
			Network: prvdr.networkURL(),
		}},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{{
				Key:   "startup-script",
				Value: &cloudConfig,
			}},
		},
	}
}

func (prvdr *Provider) parseACL(fw *compute.Firewall) (gACL, error) {
	if len(fw.SourceRanges) != 1 || len(fw.Allowed) != 3 {
		return gACL{}, errors.New("malformed firewall")
	}

	var portsStr string
	for _, allowed := range fw.Allowed {
		if allowed.IPProtocol == "icmp" {
			continue
		}

		if len(allowed.Ports) != 1 {
			return gACL{}, errors.New("malformed firewall")
		}

		if portsStr == "" {
			portsStr = allowed.Ports[0]
		} else if portsStr != allowed.Ports[0] {
			return gACL{}, errors.New("malformed firewall")
		}
	}

	var ports []int
	for _, p := range strings.Split(portsStr, "-") {
		portInt, err := strconv.Atoi(p)
		if err != nil {
			return gACL{}, fmt.Errorf("invalid port: %s", portsStr)
		}
		ports = append(ports, portInt)
	}

	acl := gACL{name: fw.Name}
	acl.CidrIP = fw.SourceRanges[0]

	switch len(ports) {
	case 1:
		acl.MinPort, acl.MaxPort = ports[0], ports[0]
	case 2:
		acl.MinPort, acl.MaxPort = ports[0], ports[1]
	default:
		return gACL{}, fmt.Errorf("invalid port: %s", portsStr)
	}

	return acl, nil
}

// SetACLs adds and removes acls in `prvdr` so that it conforms to `acls`.
func (prvdr *Provider) SetACLs(acls []acl.ACL) error {
	// Allow inter-vm communication.
	return prvdr.setACLs(append(acls, acl.ACL{CidrIP: ipv4Range, MaxPort: 65535}))
}

func (prvdr *Provider) setACLs(acls []acl.ACL) error {
	firewalls, err := prvdr.ListFirewalls(prvdr.network)
	if err != nil {
		return fmt.Errorf("list firewalls: %s", err)
	}

	var ops []*compute.Operation
	adds, removes := prvdr.planSetACLs(firewalls.Items, acls)
	for _, name := range removes {
		log.Debugf("Google Remove ACL: %s", name)
		op, err := prvdr.DeleteFirewall(name)
		if err != nil {
			return err
		}
		ops = append(ops, op)
	}

	for _, fw := range adds {
		log.Debugf("Google Add ACL: %s", fw.Name)
		op, err := prvdr.InsertFirewall(fw)
		if err != nil {
			return err
		}
		ops = append(ops, op)
	}
	return prvdr.operationWait(ops...)
}

func (prvdr *Provider) planSetACLs(cloudFWs []*compute.Firewall, acls []acl.ACL) (
	add []*compute.Firewall, remove []string) {

	var cloudACLs []gACL
	for _, fw := range cloudFWs {
		if acl, err := prvdr.parseACL(fw); err != nil {
			remove = append(remove, fw.Name)
			log.WithError(err).Debugf("Failed to parse ACL, removing: %s",
				fw.Name)
		} else {
			cloudACLs = append(cloudACLs, acl)
		}
	}

	var gacls []gACL
	for _, a := range acls {
		ip := strings.Replace(a.CidrIP, ".", "-", -1)
		ip = strings.Replace(ip, "/", "-", -1)
		name := fmt.Sprintf("%s-%s-%d-%d", prvdr.network, ip,
			a.MinPort, a.MaxPort)
		gacls = append(gacls, gACL{name: name, ACL: a})
	}

	_, adds, removes := join.HashJoin(aclSlice(gacls), aclSlice(cloudACLs), nil, nil)

	for _, i := range removes {
		remove = append(remove, i.(gACL).name)
	}

	for _, a := range adds {
		acl := a.(gACL)
		ports := fmt.Sprintf("%d-%d", acl.MinPort, acl.MaxPort)
		add = append(add, &compute.Firewall{
			Name:         acl.name,
			Network:      prvdr.networkURL(),
			Description:  prvdr.network,
			SourceRanges: []string{acl.CidrIP},
			Allowed: []*compute.FirewallAllowed{{
				IPProtocol: "tcp",
				Ports:      []string{ports},
			}, {
				IPProtocol: "udp",
				Ports:      []string{ports},
			}, {
				IPProtocol: "icmp",
			}}})
	}

	return
}

// UpdateFloatingIPs updates IPs of machines by recreating their network interfaces.
func (prvdr *Provider) UpdateFloatingIPs(machines []db.Machine) error {
	for _, m := range machines {
		instance, err := prvdr.GetInstance(prvdr.zone, m.CloudID)
		if err != nil {
			return err
		}

		// Delete existing network interface. It is only possible to assign
		// one access config per instance. Thus, updating GCE Floating IPs
		// is not a seamless, zero-downtime procedure.
		networkInterface, accessConfig, err := getNetworkConfig(instance)
		if err != nil {
			return err
		}

		// Google only supports one access config at a time, so we must wait
		// for the existing access config to be removed before adding the new
		// one.
		op, err := prvdr.DeleteAccessConfig(prvdr.zone, m.CloudID,
			accessConfig.Name, networkInterface.Name)
		if err != nil {
			return err
		}

		err = prvdr.operationWait(op)
		if err != nil {
			return errors.New(
				"timed out waiting for access config to be removed")
		}

		newAccessConfig := &compute.AccessConfig{Type: "ONE_TO_ONE_NAT"}
		if m.FloatingIP == "" {
			// Google will automatically assign a dynamic IP
			// if no NatIP is provided.
			newAccessConfig.Name = ephemeralIPName
		} else {
			newAccessConfig.Name = floatingIPName
			newAccessConfig.NatIP = m.FloatingIP
		}

		// Add new network interface.
		op, err = prvdr.AddAccessConfig(prvdr.zone, m.CloudID,
			networkInterface.Name, newAccessConfig)
		if err != nil {
			return err
		}

		err = prvdr.operationWait(op)
		if err != nil {
			return errors.New(
				"timed out waiting for new access config to be assigned")
		}
	}

	return nil
}

// Cleanup removes unnecessary detritus from this provider.  It's intended to be called
// when there are no VMs running or expected to be running soon.
func (prvdr *Provider) Cleanup() error {
	list, err := prvdr.ListNetworks(prvdr.network)
	if err != nil || len(list.Items) == 0 {
		return err
	}

	if err := prvdr.setACLs(nil); err != nil {
		return err
	}

	log.Debugf("Google Delete Network: %s", prvdr.network)
	op, err := prvdr.DeleteNetwork(prvdr.network)
	if err != nil {
		return err
	}
	return prvdr.operationWait(op)
}

func (prvdr *Provider) createNetwork() error {
	list, err := prvdr.ListNetworks(prvdr.network)
	if err != nil || len(list.Items) > 0 {
		return err
	}

	log.Debug("Google Create Network")
	op, err := prvdr.InsertNetwork(&compute.Network{
		Name:      prvdr.network,
		IPv4Range: ipv4Range,
	})
	if err != nil {
		return err
	}

	return prvdr.operationWait(op)
}

func (prvdr Provider) networkURL() string {
	return fmt.Sprintf("global/networks/%s", prvdr.network)
}

var randName = randNameImpl

func randNameImpl() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err) // This really shouldn't ever happen.
	}
	return fmt.Sprintf("k%s", strings.ToLower(base32.StdEncoding.EncodeToString(b)))
}

type gACL struct {
	name string
	acl.ACL
}

type aclSlice []gACL

func (s aclSlice) Get(i int) interface{} { return s[i] }

func (s aclSlice) Len() int { return len(s) }
