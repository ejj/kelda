package digitalocean

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digitalocean/godo"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/cloudcfg"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"

	"golang.org/x/oauth2"

	log "github.com/Sirupsen/logrus"
)

// Digital Ocean Setup
// 1. Create a new key at cloud.digitalocean.com/settings/api/tokens
// 2. Copy and paste the key into this file: ~/.digitalocean/key

// DefaultRegion is assigned to Machines without a specified region
const DefaultRegion string = "sfo1"

// Regions supported by the Digital Ocean API
var Regions = []string{"ams1", "ams2", "ams3", "blr1", "fra1", "lon1", "nyc1", "nyc2",
	"nyc3", "sfo1", "sfo2", "sgp1", "tor1"}

var apiKeyPath = ".digitalocean/key"

var image = "ubuntu-16-04-x64"

// The Cluster object represents a connection to DigitalOcean.
type Cluster struct {
	client    client
	namespace string
	region    string
}

// New starts a new client session with the API key provided in ~/.digitalocean/key.
func New(namespace, region string) (*Cluster, error) {
	clst, err := newDigitalOcean(namespace, region)
	if err != nil {
		return clst, err
	}

	_, _, err = clst.client.ListDroplets(&godo.ListOptions{})
	return clst, err
}

// Creation is broken out for unit testing.
var newDigitalOcean = func(namespace, region string) (*Cluster, error) {
	namespace = strings.ToLower(strings.Replace(namespace, "_", "-", -1))
	keyFile := filepath.Join(os.Getenv("HOME"), apiKeyPath)
	key, err := util.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)

	tc := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: key})
	oauthClient := oauth2.NewClient(oauth2.NoContext, tc)
	api := godo.NewClient(oauthClient)

	clst := &Cluster{
		namespace: namespace,
		region:    region,
		client:    doClient{droplets: api.Droplets, images: api.Images}}
	return clst, nil
}

// List will fetch all droplets that have the same name as the cluster namespace.
func (clst Cluster) List() ([]machine.Machine, error) {
	machines := []machine.Machine{}
	opt := &godo.ListOptions{} // Keep track of the page we're on.

	// DigitalOcean's API has a paginated list of droplets.
	for {
		droplets, resp, err := clst.client.ListDroplets(opt)
		if err != nil {
			return nil, err
		}

		for _, d := range droplets {
			if d.Name != clst.namespace || d.Region.Slug != clst.region {
				continue
			}

			pubIP, err := d.PublicIPv4()
			if err != nil {
				return nil, err
			}

			privIP, err := d.PrivateIPv4()
			if err != nil {
				return nil, err
			}

			machine := machine.Machine{
				ID:        strconv.Itoa(d.ID),
				PublicIP:  pubIP,
				PrivateIP: privIP,
				Size:      d.SizeSlug,
				Provider:  db.DigitalOcean,
				Region:    d.Region.Slug,
				Reserved:  true,
			}
			machines = append(machines, machine)
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		opt.Page = page + 1
	}
	return machines, nil
}

// Boot will boot every machine in a goroutine, and wait for the machines to come up.
func (clst Cluster) Boot(bootSet []machine.Machine) error {
	errChan := make(chan error, len(bootSet))
	for _, m := range bootSet {
		go func(m machine.Machine) {
			if m.Region != clst.region {
				panic("Not Reached")
			}
			errChan <- clst.createAndAttach(m)
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
func (clst Cluster) createAndAttach(m machine.Machine) error {
	cloudConfig := cloudcfg.Ubuntu(m.SSHKeys, "xenial", m.Role)
	createReq := &godo.DropletCreateRequest{
		Name:              clst.namespace,
		Region:            m.Region,
		Size:              m.Size,
		Image:             godo.DropletCreateImage{Slug: image},
		PrivateNetworking: true,
		UserData:          cloudConfig,
	}

	d, _, err := clst.client.CreateDroplet(createReq)
	if err != nil {
		return err
	}

	pred := func() bool {
		d, _, err := clst.client.GetDroplet(d.ID)
		return err == nil && d.Status == "active"
	}
	return util.WaitFor(pred, 10*time.Second, 2*time.Minute)
}

// UpdateFloatingIPs currently returns an error.
func (clst Cluster) UpdateFloatingIPs(machines []machine.Machine) error {
	return errors.New("digitalOcean floating IPs are unimplemented")
}

// Stop stops each machine and deletes their attached volumes.
func (clst Cluster) Stop(machines []machine.Machine) error {
	errChan := make(chan error, len(machines))
	for _, m := range machines {
		if m.Region != clst.region {
			panic("Not Reached")
		}

		go func(m machine.Machine) {
			errChan <- clst.deleteAndWait(m.ID)
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

func (clst Cluster) deleteAndWait(ids string) error {
	id, err := strconv.Atoi(ids)
	if err != nil {
		return err
	}

	_, err = clst.client.DeleteDroplet(id)
	if err != nil {
		return err
	}

	pred := func() bool {
		d, _, err := clst.client.GetDroplet(id)
		return err != nil || d == nil
	}
	return util.WaitFor(pred, 500*time.Millisecond, 1*time.Minute)
}

// SetACLs is not supported in DigitalOcean.
func (clst Cluster) SetACLs(acls []acl.ACL) error {
	log.Debug("DigitalOcean does not support ACLs")
	return nil
}
