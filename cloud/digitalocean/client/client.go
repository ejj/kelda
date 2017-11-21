//go:generate mockery -name=Client

package client

import (
	"context"
	"net/http"

	"github.com/digitalocean/godo"
	"github.com/kelda/kelda/counter"
)

// A Client for DigitalOcean's API. Used for unit testing.
type Client interface {
	CreateDroplet(*godo.DropletCreateRequest) (*godo.Droplet, *godo.Response, error)
	DeleteDroplet(int) (*godo.Response, error)
	GetDroplet(int) (*godo.Droplet, *godo.Response, error)
	ListDroplets(*godo.ListOptions, string) ([]godo.Droplet, *godo.Response, error)

	CreateTag(string) (*godo.Tag, *godo.Response, error)

	ListFloatingIPs(*godo.ListOptions) ([]godo.FloatingIP, *godo.Response, error)
	AssignFloatingIP(string, int) (*godo.Action, *godo.Response, error)
	UnassignFloatingIP(string) (*godo.Action, *godo.Response, error)

	CreateFirewall(string, []godo.OutboundRule, []godo.InboundRule) (*godo.Firewall,
		*godo.Response, error)
	ListFirewalls(*godo.ListOptions) ([]godo.Firewall, *godo.Response, error)
	AddRules(string, []godo.InboundRule) (*godo.Response, error)
	RemoveRules(string, []godo.InboundRule) (*godo.Response, error)
}

type client struct {
	droplets          godo.DropletsService
	floatingIPs       godo.FloatingIPsService
	floatingIPActions godo.FloatingIPActionsService
	acls              godo.FirewallsService
	tags              godo.TagsService
}

var c = counter.New("Digital Ocean")

func (client client) CreateDroplet(req *godo.DropletCreateRequest) (*godo.Droplet,
	*godo.Response, error) {
	c.Inc("Create Droplet")
	return client.droplets.Create(context.Background(), req)
}

func (client client) DeleteDroplet(id int) (*godo.Response, error) {
	c.Inc("Delete Droplet")
	return client.droplets.Delete(context.Background(), id)
}

func (client client) GetDroplet(id int) (*godo.Droplet, *godo.Response, error) {
	c.Inc("Get Droplet")
	return client.droplets.Get(context.Background(), id)
}

func (client client) ListDroplets(opt *godo.ListOptions, tag string) ([]godo.Droplet,
	*godo.Response, error) {
	c.Inc("List Droplets")
	return client.droplets.ListByTag(context.Background(), tag, opt)
}

func (client client) CreateTag(name string) (*godo.Tag, *godo.Response, error) {
	c.Inc("Create Tag")
	return client.tags.Create(context.Background(),
		&godo.TagCreateRequest{
			Name: name,
		},
	)
}

func (client client) ListFloatingIPs(opt *godo.ListOptions) ([]godo.FloatingIP,
	*godo.Response, error) {
	c.Inc("List Floating IPs")
	return client.floatingIPs.List(context.Background(), opt)
}

func (client client) AssignFloatingIP(ip string, id int) (*godo.Action,
	*godo.Response, error) {
	c.Inc("Assign Floating IP")
	return client.floatingIPActions.Assign(context.Background(), ip, id)
}

func (client client) UnassignFloatingIP(ip string) (*godo.Action, *godo.Response,
	error) {
	c.Inc("Remove Floating IP")
	return client.floatingIPActions.Unassign(context.Background(), ip)
}

func (client client) AddRules(id string, rules []godo.InboundRule) (*godo.Response,
	error) {

	c.Inc("Add Rules")
	return client.acls.AddRules(context.Background(), id, &godo.FirewallRulesRequest{
		InboundRules: rules,
	})
}

func (client client) RemoveRules(id string, rules []godo.InboundRule) (*godo.Response,
	error) {

	c.Inc("Remove Rules")
	return client.acls.RemoveRules(context.Background(), id,
		&godo.FirewallRulesRequest{
			InboundRules: rules,
		},
	)
}

func (client client) CreateFirewall(tag string, outbound []godo.OutboundRule,
	inbound []godo.InboundRule) (*godo.Firewall, *godo.Response, error) {

	c.Inc("Create Firewall")
	req := &godo.FirewallRequest{
		Name:          tag,
		OutboundRules: outbound,
		InboundRules:  inbound,
		Tags:          []string{tag},
	}
	return client.acls.Create(context.Background(), req)
}

func (client client) ListFirewalls(opt *godo.ListOptions) ([]godo.Firewall,
	*godo.Response, error) {

	c.Inc("List Firewalls")
	return client.acls.List(context.Background(), opt)
}

// New creates a new DigitalOcean client.
func New(oauthClient *http.Client) Client {
	api := godo.NewClient(oauthClient)
	return client{
		droplets:          api.Droplets,
		floatingIPs:       api.FloatingIPs,
		floatingIPActions: api.FloatingIPActions,
		acls:              api.Firewalls,
		tags:              api.Tags,
	}
}
