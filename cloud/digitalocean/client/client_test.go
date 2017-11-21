package client

import (
	"errors"
	"net/http"
	"testing"

	"github.com/digitalocean/godo"
	"github.com/stretchr/testify/assert"
)

type rtErr struct{}

func (r rtErr) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("test")
}

func TestError(t *testing.T) {
	c := New(&http.Client{Transport: rtErr{}})

	_, _, err := c.CreateDroplets(&godo.DropletMultiCreateRequest{})
	assert.EqualError(t, err, "Post https://api.digitalocean.com/v2/droplets: test")

	_, err = c.DeleteDroplet(3)
	assert.EqualError(t, err,
		"Delete https://api.digitalocean.com/v2/droplets/3: test")

	_, _, err = c.GetDroplet(3)
	assert.EqualError(t, err, "Get https://api.digitalocean.com/v2/droplets/3: test")

	_, _, err = c.ListDroplets(&godo.ListOptions{}, "tag")
	assert.EqualError(t, err,
		"Get https://api.digitalocean.com/v2/droplets?tag_name=tag: test")

	_, _, err = c.ListFloatingIPs(&godo.ListOptions{})
	assert.EqualError(t, err,
		"Get https://api.digitalocean.com/v2/floating_ips: test")

	_, _, err = c.AssignFloatingIP("a", 3)
	assert.EqualError(t, err,
		"Post https://api.digitalocean.com/v2/floating_ips/a/actions: test")

	_, _, err = c.UnassignFloatingIP("a")
	assert.EqualError(t, err,
		"Post https://api.digitalocean.com/v2/floating_ips/a/actions: test")

	_, _, err = c.CreateFirewall("a", nil, nil)
	assert.EqualError(t, err,
		"Post https://api.digitalocean.com/v2/firewalls: test")

	_, _, err = c.ListFirewalls(nil)
	assert.EqualError(t, err,
		"Get https://api.digitalocean.com/v2/firewalls: test")

	_, err = c.AddRules("a", nil)
	assert.EqualError(t, err,
		"Post https://api.digitalocean.com/v2/firewalls/a/rules: test")

	_, err = c.RemoveRules("a", nil)
	assert.EqualError(t, err,
		"Delete https://api.digitalocean.com/v2/firewalls/a/rules: test")
}
