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
	"github.com/kelda/kelda/cloud/wait"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"

	"golang.org/x/oauth2"
)

// DefaultRegion is assigned to Machines without a specified region
const DefaultRegion string = "sfo1"

// Regions supported by the Digital Ocean API
var Regions = []string{"nyc1", "nyc2", "lon1", "sfo1", "sfo2"}

var c = counter.New("Digital Ocean")

var apiKeyPath = ".digitalocean/key"

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
func (prvdr Provider) Boot(bootSet []db.Machine) error {
	errChan := make(chan error, len(bootSet))
	for _, m := range bootSet {
		if m.Preemptible {
			return errors.New("preemptible instances are not yet implemented")
		}

		go func(m db.Machine) {
			errChan <- prvdr.createAndAttach(m)
		}(m)
	}

	var err error
	for range bootSet {
		if e := <-errChan; e != nil {
			err = e
		}
	}
	return err
}

// Creates a new machine, and waits for the machine to become active.
func (prvdr Provider) createAndAttach(m db.Machine) error {
	cloudConfig := cfg.Ubuntu(m, "")
	createReq := &godo.DropletCreateRequest{
		Name:              prvdr.namespace,
		Region:            prvdr.region,
		Size:              m.Size,
		Image:             godo.DropletCreateImage{ID: imageID},
		PrivateNetworking: true,
		UserData:          cloudConfig,
	}

	d, _, err := prvdr.CreateDroplet(createReq)
	if err != nil {
		return err
	}

	pred := func() bool {
		d, _, err := prvdr.GetDroplet(d.ID)
		return err == nil && d.Status == "active"
	}
	return wait.Wait(pred)
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
		go func(m db.Machine) {
			errChan <- prvdr.deleteAndWait(m.CloudID)
		}(m)
	}

	var err error
	for range machines {
		if e := <-errChan; e != nil {
			err = e
		}
	}
	return err
}

func (prvdr Provider) deleteAndWait(ids string) error {
	id, err := strconv.Atoi(ids)
	if err != nil {
		return err
	}

	_, err = prvdr.DeleteDroplet(id)
	if err != nil {
		return err
	}

	pred := func() bool {
		d, _, err := prvdr.GetDroplet(id)
		return err != nil || d == nil
	}
	return wait.Wait(pred)
}

// SetACLs is not supported in DigitalOcean.
func (prvdr Provider) SetACLs(acls []acl.ACL) error {
	return nil
}
