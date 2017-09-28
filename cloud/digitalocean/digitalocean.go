package digitalocean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/digitalocean/godo"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/digitalocean/client"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"

	"golang.org/x/oauth2"

	log "github.com/sirupsen/logrus"
)

// DefaultRegion is assigned to Machines without a specified region
const DefaultRegion string = "sfo1"

// Regions supported by the Digital Ocean API
var Regions = []string{"nyc1", "nyc2", "lon1", "sfo1", "sfo2"}

var c = counter.New("Digital Ocean")

var apiKeyPath = ".digitalocean/key"

var (
	// When creating firewall rules, the API requires that each rule have a protocol
	// associated with it. It accepts the ones listed below, and we want to allow
	// traffic only based on IP and port, so allow them all.
	//
	// https://developers.digitalocean.com/documentation/v2/#add-rules-to-a-firewall
	protocols = []string{"tcp", "udp", "icmp"}

	allIPs = &godo.Destinations{
		Addresses: []string{"0.0.0.0/0", "::/0"},
	}

	// DigitalOcean firewalls block all traffic that is not explicitly allowed. We
	// want to allow all outgoing traffic.
	allowAll = []godo.OutboundRule{
		{
			Protocol:     "tcp",
			PortRange:    "all",
			Destinations: allIPs,
		},
		{
			Protocol:     "udp",
			PortRange:    "all",
			Destinations: allIPs,
		},
		{
			Protocol:     "icmp",
			Destinations: allIPs,
		},
	}
)

// 16.04.1 x64 created at 2017-02-03.
var imageID = 22601368

// The Provider object represents a connection to DigitalOcean.
type Provider struct {
	client.Client

	namespace string
	region    string
}

// New starts a new client session with the API key provided in ~/.digitalocean/key.
func New(namespace, region string) (*Provider, error) {
	prvdr, err := newDigitalOcean(namespace, region)
	if err != nil {
		return prvdr, err
	}

	_, _, err = prvdr.ListDroplets(&godo.ListOptions{})
	return prvdr, err
}

// Creation is broken out for unit testing.
var newDigitalOcean = func(namespace, region string) (*Provider, error) {
	namespace = strings.ToLower(strings.Replace(namespace, "_", "-", -1))
	keyFile := filepath.Join(os.Getenv("HOME"), apiKeyPath)
	key, err := util.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)

	tc := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: key})
	oauthClient := oauth2.NewClient(oauth2.NoContext, tc)

	prvdr := &Provider{
		namespace: namespace,
		region:    region,
		Client:    client.New(oauthClient),
	}
	return prvdr, nil
}

// List will fetch all droplets that have the same name as the cluster namespace.
func (prvdr Provider) List() (machines []db.Machine, err error) {
	floatingIPs, err := prvdr.getFloatingIPs()
	if err != nil {
		return nil, err
	}

	dropletListOpt := &godo.ListOptions{} // Keep track of the page we're on.
	// DigitalOcean's API has a paginated list of droplets.
	for {
		droplets, resp, err := prvdr.ListDroplets(dropletListOpt)
		if err != nil {
			return nil, fmt.Errorf("list droplets: %s", err)
		}

		for _, d := range droplets {
			if d.Name != prvdr.namespace || d.Region.Slug != prvdr.region {
				continue
			}

			pubIP, err := d.PublicIPv4()
			if err != nil {
				return nil, fmt.Errorf("get public IP: %s", err)
			}

			privIP, err := d.PrivateIPv4()
			if err != nil {
				return nil, fmt.Errorf("get private IP: %s", err)
			}

			machine := db.Machine{
				Provider:    db.DigitalOcean,
				Region:      prvdr.region,
				CloudID:     strconv.Itoa(d.ID),
				PublicIP:    pubIP,
				PrivateIP:   privIP,
				FloatingIP:  floatingIPs[d.ID],
				Size:        d.SizeSlug,
				Preemptible: false,
			}
			machines = append(machines, machine)
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		dropletListOpt.Page++
	}
	return machines, nil
}

func (prvdr Provider) getFloatingIPs() (map[int]string, error) {
	floatingIPListOpt := &godo.ListOptions{}
	floatingIPs := map[int]string{}
	for {
		ips, resp, err := prvdr.ListFloatingIPs(floatingIPListOpt)
		if err != nil {
			return nil, fmt.Errorf("list floating IPs: %s", err)
		}

		for _, ip := range ips {
			if ip.Droplet == nil {
				continue
			}
			floatingIPs[ip.Droplet.ID] = ip.IP
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		floatingIPListOpt.Page++
	}

	return floatingIPs, nil
}

// Boot will boot every machine in a goroutine, and wait for the machines to come up.
func (prvdr Provider) Boot(bootSet []db.Machine) ([]string, error) {
	var ids []string
	for _, m := range bootSet {
		if m.Preemptible {
			return ids, errors.New(
				"preemptible instances are not implemented")
		}

		createReq := &godo.DropletCreateRequest{
			Name:              prvdr.namespace,
			Region:            prvdr.region,
			Size:              m.Size,
			Image:             godo.DropletCreateImage{ID: imageID},
			PrivateNetworking: true,
			UserData:          cfg.Ubuntu(m, ""),
			Tags:              []string{prvdr.getTag()},
		}
		d, _, err := prvdr.CreateDroplet(createReq)
		if err != nil {
			return ids, err
		}

		ids = append(ids, strconv.Itoa(d.ID))
	}

	return ids, nil
}

// Returns a unique tag to use for all entities in this namespace and region.
func (prvdr Provider) getTag() string {
	return fmt.Sprintf("%s-%s", prvdr.namespace, prvdr.region)
}

// UpdateFloatingIPs updates Droplet to Floating IP associations.
func (prvdr Provider) UpdateFloatingIPs(desired []db.Machine) error {
	curr, err := prvdr.List()
	if err != nil {
		return fmt.Errorf("list machines: %s", err)
	}

	return prvdr.syncFloatingIPs(curr, desired)
}

func (prvdr Provider) syncFloatingIPs(curr, targets []db.Machine) error {
	idKey := func(intf interface{}) interface{} {
		return intf.(db.Machine).CloudID
	}
	pairs, _, unmatchedDesired := join.HashJoin(
		db.MachineSlice(curr), db.MachineSlice(targets), idKey, idKey)

	if len(unmatchedDesired) != 0 {
		var unmatchedIDs []string
		for _, m := range unmatchedDesired {
			unmatchedIDs = append(unmatchedIDs, m.(db.Machine).CloudID)
		}
		return fmt.Errorf("no matching IDs: %s", strings.Join(unmatchedIDs, ", "))
	}

	for _, pair := range pairs {
		curr := pair.L.(db.Machine)
		desired := pair.R.(db.Machine)

		if curr.FloatingIP == desired.FloatingIP {
			continue
		}

		if curr.FloatingIP != "" {
			_, _, err := prvdr.UnassignFloatingIP(curr.FloatingIP)
			if err != nil {
				return fmt.Errorf("unassign IP (%s): %s",
					curr.FloatingIP, err)
			}
		}

		if desired.FloatingIP != "" {
			id, err := strconv.Atoi(curr.CloudID)
			if err != nil {
				return fmt.Errorf("malformed id (%s): %s",
					curr.CloudID, err)
			}

			_, _, err = prvdr.AssignFloatingIP(desired.FloatingIP, id)
			if err != nil {
				return fmt.Errorf("assign IP (%s to %d): %s",
					desired.FloatingIP, id, err)
			}
		}
	}

	return nil
}

// Stop stops each machine and deletes their attached volumes.
func (prvdr Provider) Stop(machines []db.Machine) error {
	for _, m := range machines {
		id, err := strconv.Atoi(m.CloudID)
		if err != nil {
			return err
		}

		_, err = prvdr.DeleteDroplet(id)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetACLs adds and removes acls in `prvdr` so that it conforms to `acls`.
func (prvdr Provider) SetACLs(acls []acl.ACL) error {
	firewall, err := prvdr.getCreateFirewall()
	if err != nil {
		return err
	}

	add, remove := syncACLs(acls, firewall.InboundRules)

	if len(add) > 0 {
		if _, err := prvdr.AddRules(firewall.ID, add); err != nil {
			return err
		}
	}
	if len(remove) > 0 {
		if _, err := prvdr.RemoveRules(firewall.ID, remove); err != nil {
			return err
		}
	}
	return nil
}

func (prvdr Provider) getCreateFirewall() (*godo.Firewall, error) {
	tagName := prvdr.getTag()
	firewallListOpt := &godo.ListOptions{}
	for {
		firewalls, resp, err := prvdr.ListFirewalls(firewallListOpt)
		if err != nil {
			return nil, fmt.Errorf("list firewalls: %s", err)
		}

		for _, firewall := range firewalls {
			if firewall.Name == tagName {
				return &firewall, nil
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		firewallListOpt.Page++
	}

	_, _, err := prvdr.CreateTag(tagName)
	if err != nil {
		return nil, err
	}

	// The outbound rules and inter-droplet inbound rules are generated only
	// once: when the firewall is first created. If these rules are externally
	// deleted, there will be errors unless the firewall is destroyed
	// (and then recreated by the daemon).
	internalDroplets := &godo.Sources{
		Tags: []string{tagName},
	}
	allowInternal := []godo.InboundRule{
		{
			Protocol:  "tcp",
			PortRange: "all",
			Sources:   internalDroplets,
		},
		{
			Protocol:  "udp",
			PortRange: "all",
			Sources:   internalDroplets,
		},
	}
	firewall, _, err := prvdr.CreateFirewall(tagName, allowAll, allowInternal)
	return firewall, err
}

func syncACLs(desired []acl.ACL, current []godo.InboundRule) (
	addRules, removeRules []godo.InboundRule) {

	curACLSet := map[acl.ACL]struct{}{}
	for _, cur := range current {
		ports := strings.Split(cur.PortRange, "-")
		if len(ports) == 1 {
			ports = []string{ports[0], ports[0]}
		}

		from, err := strconv.Atoi(ports[0])
		if err != nil {
			log.WithError(err).WithField("port", ports[0]).Warn(
				"Failed to parse from port of InboundRule")
			continue
		}

		to, err := strconv.Atoi(ports[1])
		if err != nil {
			log.WithError(err).WithField("port", ports[1]).Warn(
				"Failed to parse to port of InboundRule")
			continue
		}

		for _, addr := range cur.Sources.Addresses {
			key := acl.ACL{
				CidrIP:  addr,
				MinPort: int(from),
				MaxPort: int(to),
			}
			curACLSet[key] = struct{}{}
		}
	}

	var curACLs acl.Slice
	for key := range curACLSet {
		curACLs = append(curACLs, key)
	}

	_, toAdd, toRemove := join.HashJoin(acl.Slice(desired), curACLs, nil, nil)

	var add, remove []acl.ACL
	for _, intf := range toAdd {
		add = append(add, intf.(acl.ACL))
	}
	for _, intf := range toRemove {
		remove = append(remove, intf.(acl.ACL))
	}
	return toRules(add), toRules(remove)
}

func toRules(acls []acl.ACL) (rules []godo.InboundRule) {
	icmpSources := map[string]struct{}{}

	for _, acl := range acls {
		for _, proto := range protocols {
			portRange := fmt.Sprintf("%d-%d", acl.MinPort, acl.MaxPort)
			if acl.MinPort == acl.MaxPort {
				portRange = fmt.Sprintf("%d", acl.MinPort)
				if acl.MinPort == 0 {
					portRange = "all"
				}
			}

			if proto == "icmp" {
				if _, ok := icmpSources[acl.CidrIP]; ok {
					continue
				}
				icmpSources[acl.CidrIP] = struct{}{}
				portRange = ""
			}

			rules = append(rules, godo.InboundRule{
				Protocol:  proto,
				PortRange: portRange,
				Sources: &godo.Sources{
					Addresses: []string{acl.CidrIP},
				},
			})
		}
	}

	return rules
}

func machineAction(m db.Machine, fn func(db.Machine) error) func() error {
	return func() error {
		return fn(m)
	}
}
