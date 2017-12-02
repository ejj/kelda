package client

import (
	"errors"
	"net/http"
	"testing"

	compute "google.golang.org/api/compute/v1"

	"github.com/stretchr/testify/assert"
)

type rtErr struct{}

func (r rtErr) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("test")
}

func TestErrors(t *testing.T) {

	service, err := compute.New(&http.Client{Transport: rtErr{}})
	assert.NoError(t, err)

	c := Client(&client{gce: service, projID: "pid"})

	url := "https://www.googleapis.com/compute/v1/projects/pid/"
	zone := url + "zones/z/"
	inst := zone + "instances/i"

	_, err = c.GetInstance("z", "i")
	assert.EqualError(t, err, "Get "+inst+"?alt=json: test")

	_, err = c.ListInstances("z", "f")
	assert.EqualError(t, err, "Get "+zone+
		"instances?alt=json&filter=description+eq+f: test")

	_, err = c.InsertInstance("z", nil)
	assert.EqualError(t, err, "Post "+zone+"instances?alt=json: test")

	_, err = c.DeleteInstance("z", "i")
	assert.EqualError(t, err, "Delete "+inst+"?alt=json: test")

	_, err = c.AddAccessConfig("z", "i", "ni", nil)
	assert.EqualError(t, err, "Post "+inst+
		"/addAccessConfig?alt=json&networkInterface=ni: test")

	_, err = c.DeleteAccessConfig("z", "i", "ac", "ni")
	assert.EqualError(t, err, "Post "+inst+
		"/deleteAccessConfig?accessConfig=ac&alt=json&networkInterface=ni: test")

	_, err = c.GetZoneOperation("z", "o")
	assert.EqualError(t, err, "Get "+zone+"operations/o?alt=json: test")

	_, err = c.GetGlobalOperation("o")
	assert.EqualError(t, err, "Get "+url+"global/operations/o?alt=json: test")

	_, err = c.ListFirewalls("n")
	assert.EqualError(t, err, "Get "+url+
		"global/firewalls?alt=json&filter=description+eq+n: test")

	_, err = c.InsertFirewall(nil)
	assert.EqualError(t, err, "Post "+url+"global/firewalls?alt=json: test")

	_, err = c.PatchFirewall("", nil)
	assert.EqualError(t, err, "Patch "+url+"global/firewalls/?alt=json: test")

	_, err = c.DeleteFirewall("f")
	assert.EqualError(t, err, "Delete "+url+"global/firewalls/f?alt=json: test")

	_, err = c.ListNetworks("n")
	assert.EqualError(t, err, "Get "+url+
		"global/networks?alt=json&filter=name+eq+n: test")

	_, err = c.InsertNetwork(nil)
	assert.EqualError(t, err, "Post "+url+"global/networks?alt=json: test")
}

func TestGetProjectID(t *testing.T) {
	_, err := getProjectID("malformed")
	assert.Error(t, err, "")

	_, err = getProjectID(`{"no": "project_id"}`)
	assert.Error(t, err, "")

	id, err := getProjectID(`{"project_id": "myid"}`)
	assert.NoError(t, err)
	assert.Equal(t, "myid", id)
}
