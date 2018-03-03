package digitalocean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalocean/godo"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/digitalocean/client"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"
	log "github.com/sirupsen/logrus"

	"golang.org/x/oauth2"
)

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

// 16.04.1 x64 created at 2016-12-21.
var imageID = 21669205

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

var listAllMutex sync.Mutex
var listAllTimeout time.Time
var listAllMachines []taggedMachine
var listAllErr error

// List will fetch all droplets that have the same name as the cluster namespace.
func (prvdr Provider) List() (machines []db.Machine, err error) {
	listAllMutex.Lock()
	if time.Now().After(listAllTimeout) {
		listAllMachines, listAllErr = prvdr.listAll()
		listAllTimeout = time.Now().Add(1 * second)
	} else {
		c.Inc("List Cache Hit")
	}
	taggedMachines, err := listAllMachines, listAllErr
	listAllMutex.Unlock()

	if err != nil {
		return nil, err
	}

	myTag := prvdr.getTag()
	for _, tm := range taggedMachines {
		for _, tag := range tm.tags {
			if tag == myTag {
				machines = append(machines, tm.Machine)
				break
			}
		}
	}

	return machines, err
}

type taggedMachine struct {
	db.Machine
	tags []string
}

func (prvdr Provider) listAll() (machines []taggedMachine, err error) {
	floatingIPs, err := prvdr.getFloatingIPs()
	if err != nil {
		return nil, err
	}

	// Keep track of the page we're on.
	// DigitalOcean's API has a paginated list of droplets.
	dropletListOpt := &godo.ListOptions{Page: 1, PerPage: 200}
	for {
		droplets, resp, err := prvdr.ListDroplets(dropletListOpt)
		if err != nil {
			return nil, fmt.Errorf("list droplets: %s", err)
		}

		for _, d := range droplets {
			pubIP, err := d.PublicIPv4()
			if err != nil {
				return nil, fmt.Errorf("get public IP: %s", err)
			}

			privIP, err := d.PrivateIPv4()
			if err != nil {
				return nil, fmt.Errorf("get private IP: %s", err)
			}

			machine := taggedMachine{
				Machine: db.Machine{
					Provider:    db.DigitalOcean,
					Region:      d.Region.Slug,
					CloudID:     strconv.Itoa(d.ID),
					PublicIP:    pubIP,
					PrivateIP:   privIP,
					FloatingIP:  floatingIPs[d.ID],
					Size:        d.SizeSlug,
					Preemptible: false,
				},
				tags: d.Tags,
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
	floatingIPListOpt := &godo.ListOptions{Page: 1, PerPage: 200}
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
func (prvdr Provider) Boot(machines []db.Machine) ([]string, error) {
	type bootRequest struct {
		size     string
		userData string
	}

	bootSet := map[bootRequest]int{}
	for _, m := range machines {
		if m.Preemptible {
			err := errors.New("preemptible instances are not implemented")
			return nil, err
		}

		br := bootRequest{size: m.Size, userData: cfg.Ubuntu(m, "")}
		bootSet[br] = bootSet[br] + 1
	}

	var reqs []godo.DropletMultiCreateRequest
	for br, count := range bootSet {
		// Digital Ocean has an arbitrary limit of 10 on the number of droplets
		// that can be created in a single request.
		for count > 0 {
			n := count
			if n > 10 {
				n = 10
			}
			count -= n

			var names []string
			for i := 0; i < n; i++ {
				names = append(names, "Kelda")
			}

			reqs = append(reqs, godo.DropletMultiCreateRequest{
				Names:             names,
				Region:            prvdr.region,
				Size:              br.size,
				Image:             godo.DropletCreateImage{ID: imageID},
				PrivateNetworking: true,
				UserData:          br.userData,
				Tags:              []string{prvdr.getTag()}})
		}
	}

	errChan := make(chan error, len(reqs))
	dropChan := make(chan []godo.Droplet, len(reqs))
	for _, req := range reqs {
		go func(req godo.DropletMultiCreateRequest) {
			droplets, _, err := prvdr.CreateDroplets(&req)
			dropChan <- droplets
			errChan <- err
		}(req)
	}

	for i := 0; i < len(reqs); i++ {
		if err := <-errChan; err != nil {
			return nil, err
		}
	}

	close(dropChan) // So we can range over it.
	var ids []string
	for droplets := range dropChan {
		for _, d := range droplets {
			ids = append(ids, strconv.Itoa(d.ID))
		}
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
	errChan := make(chan error, len(machines))
	for _, m := range machines {
		id, err := strconv.Atoi(m.CloudID)
		if err != nil {
			return err
		}

		go func(id int) {
			_, err = prvdr.DeleteDroplet(id)
			errChan <- err
		}(id)
	}

	for i := 0; i < len(machines); i++ {
		if err := <-errChan; err != nil {
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

	add, remove := prvdr.syncACLs(acls, firewall.InboundRules)

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

// Cleanup removes unnecessary detritus from this provider.  It's intended to be called
// when there are no VMS running or expected to be running soon.
func (prvdr Provider) Cleanup() error {
	fw, err := prvdr.getFirewall()
	if err == nil && fw != nil {
		log.WithFields(log.Fields{
			"name": fw.Name,
			"id":   fw.ID,
		}).Debug("DigitalOcean Delete Firewall")
		_, err = prvdr.DeleteFirewall(fw.ID)
	}
	return err
}

func (prvdr Provider) getCreateFirewall() (*godo.Firewall, error) {
	firewall, err := prvdr.getFirewall()
	if err != nil {
		return nil, err
	}

	if firewall != nil {
		return firewall, nil
	}

	tag := prvdr.getTag()
	_, _, err = prvdr.CreateTag(tag)
	if err != nil {
		return nil, err
	}

	// The outbound rules are generated only once: when the firewall is first
	// created. If these rules are externally deleted, there will be errors
	// unless the firewall is destroyed (and then recreated by the daemon).
	firewall, _, err = prvdr.CreateFirewall(tag, allowAll, nil)
	return firewall, err
}

func (prvdr Provider) getFirewall() (*godo.Firewall, error) {
	tag := prvdr.getTag()
	firewallListOpt := &godo.ListOptions{Page: 1, PerPage: 200}
	for {
		firewalls, resp, err := prvdr.ListFirewalls(firewallListOpt)
		if err != nil {
			return nil, fmt.Errorf("list firewalls: %s", err)
		}

		for _, firewall := range firewalls {
			for _, t := range firewall.Tags {
				if t == tag {
					fixRulesPortRange(&firewall)
					return &firewall, nil
				}
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			return nil, nil
		}
		firewallListOpt.Page++
	}
}

// The DigitalOcean API is inconsistent for listing rules, and manipulating
// rules. The listing API represents "all port ranges" with "0", but when
// adding or removing rules, it requires the string "all" for TCP or UDP, and
// the empty string for ICMP.
// Therefore, we convert the rules here so that callers don't have to deal with
// converting rules into the appropriate form when removing rules from
// InboundRules.
func fixRulesPortRange(fw *godo.Firewall) {
	for i := range fw.InboundRules {
		if fw.InboundRules[i].PortRange == "0" {
			if fw.InboundRules[i].Protocol == "icmp" {
				fw.InboundRules[i].PortRange = ""
			} else {
				fw.InboundRules[i].PortRange = "all"
			}
		}
	}
}

func (prvdr Provider) syncACLs(desired []acl.ACL, current []godo.InboundRule) (
	addRules, removeRules []godo.InboundRule) {

	internalDroplets := &godo.Sources{Tags: []string{prvdr.getTag()}}
	desiredRules := append(toRules(desired),
		godo.InboundRule{
			Protocol:  "tcp",
			PortRange: "all",
			Sources:   internalDroplets,
		},
		godo.InboundRule{
			Protocol:  "udp",
			PortRange: "all",
			Sources:   internalDroplets,
		})

	key := func(intf interface{}) interface{} {
		rule := intf.(godo.InboundRule)
		return struct {
			PortRange, Protocol, Addresses, Tags string
		}{
			rule.PortRange, rule.Protocol,
			strings.Join(rule.Sources.Addresses, ","),
			strings.Join(rule.Sources.Tags, ","),
		}
	}
	_, toAdd, toRemove := join.HashJoin(inboundRuleSlice(desiredRules),
		inboundRuleSlice(current), key, key)
	for _, intf := range toAdd {
		addRules = append(addRules, intf.(godo.InboundRule))
	}
	for _, intf := range toRemove {
		removeRules = append(removeRules, intf.(godo.InboundRule))
	}
	return addRules, removeRules
}

func toRules(acls []acl.ACL) (rules []godo.InboundRule) {
	icmpSources := map[string]struct{}{}

	for _, acl := range acls {
		for _, proto := range protocols {
			portRange := fmt.Sprintf("%d-%d", acl.MinPort, acl.MaxPort)
			if acl.MinPort == acl.MaxPort {
				portRange = fmt.Sprintf("%d", acl.MinPort)
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

type inboundRuleSlice []godo.InboundRule

func (slc inboundRuleSlice) Get(ii int) interface{} {
	return slc[ii]
}

func (slc inboundRuleSlice) Len() int {
	return len(slc)
}

var second = time.Second // Mockable for unit tests
