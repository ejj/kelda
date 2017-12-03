package google

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/google/client/mocks"
	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/assert"

	compute "google.golang.org/api/compute/v1"
)

func getProvider() (*mocks.Client, Provider) {
	mc := new(mocks.Client)
	return mc, Provider{
		Client:    mc,
		namespace: "namespace",
		network:   "network",
		zone:      "zone-1",
		intFW:     "intFW",
	}
}

func TestList(t *testing.T) {
	mc, gce := getProvider()
	mc.On("ListInstances", "zone-1", "network").Return(&compute.InstanceList{
		Items: []*compute.Instance{
			{
				MachineType: "machine/split/type-1",
				Name:        "name-1",
				NetworkInterfaces: []*compute.NetworkInterface{
					{
						AccessConfigs: []*compute.AccessConfig{
							{
								NatIP: "x.x.x.x",
							},
						},
						NetworkIP: "y.y.y.y",
					},
				},
			},
		},
	}, nil)

	machines, err := gce.List()
	assert.NoError(t, err)
	assert.Len(t, machines, 1)
	assert.Equal(t, machines[0], db.Machine{
		Provider:  "Google",
		Region:    "zone-1",
		CloudID:   "name-1",
		PublicIP:  "x.x.x.x",
		PrivateIP: "y.y.y.y",
		Size:      "type-1",
	})
}

func TestListFirewalls(t *testing.T) {
	mc, gce := getProvider()
	mc.On("ListFirewalls", gce.network).Return(&compute.FirewallList{
		Items: []*compute.Firewall{
			{
				Network:    gce.networkURL(),
				Name:       "badZone",
				TargetTags: []string{"zone-2"},
			},
			{
				Network:    gce.networkURL(),
				Name:       "intFW",
				TargetTags: []string{"zone-1"},
			},
			{
				Network:    gce.networkURL(),
				Name:       "shouldReturn",
				TargetTags: []string{"zone-1"},
			},
		},
	}, nil).Once()

	fws, err := gce.listFirewalls()
	assert.NoError(t, err)
	assert.Len(t, fws, 1)
	assert.Equal(t, fws[0].Name, "shouldReturn")

	mc.On("ListFirewalls", gce.network).Return(nil, errors.New("err")).Once()
	_, err = gce.listFirewalls()
	assert.EqualError(t, err, "list firewalls: err")
}

func TestListBadNetworkInterface(t *testing.T) {
	mc, gce := getProvider()

	// Tests that List returns an error when no network interfaces are
	// configured.
	mc.On("ListInstances", "zone-1", "network").Return(&compute.InstanceList{
		Items: []*compute.Instance{
			{
				MachineType:       "machine/split/type-1",
				Name:              "name-1",
				NetworkInterfaces: []*compute.NetworkInterface{},
			},
		},
	}, nil)

	_, err := gce.List()
	assert.EqualError(t, err, "Google instances are expected to have exactly 1 "+
		"interface; for instance name-1, found 0")
}

func TestParseACLs(t *testing.T) {
	_, gce := getProvider()
	parsed, err := gce.parseACLs([]compute.Firewall{
		{
			Name: "firewall",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"80", "20-25"}},
			},
			SourceRanges: []string{"foo", "bar"},
		},
		{
			Name: "firewall2",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"1-65535"}},
			},
			SourceRanges: []string{"foo"},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []acl.ACL{
		{MinPort: 80, MaxPort: 80, CidrIP: "foo"},
		{MinPort: 20, MaxPort: 25, CidrIP: "foo"},
		{MinPort: 80, MaxPort: 80, CidrIP: "bar"},
		{MinPort: 20, MaxPort: 25, CidrIP: "bar"},
		{MinPort: 1, MaxPort: 65535, CidrIP: "foo"},
	}, parsed)

	_, err = gce.parseACLs([]compute.Firewall{
		{
			Name: "firewall",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"NaN"}},
			},
			SourceRanges: []string{"foo"},
		},
	})
	assert.EqualError(t, err, `parse ports of firewall: parse ints: strconv.Atoi: `+
		`parsing "NaN": invalid syntax`)

	_, err = gce.parseACLs([]compute.Firewall{
		{
			Name: "firewall",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"1-80-81"}},
			},
			SourceRanges: []string{"foo"},
		},
	})
	assert.EqualError(t, err,
		"parse ports of firewall: unrecognized port format: 1-80-81")
}
