package google

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/google/client/mocks"
	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func TestBoot(t *testing.T) {
	mc, gce := getProvider()

	_, err := gce.Boot([]db.Machine{{Preemptible: true}})
	assert.EqualError(t, err, "preemptible vms are not implemented")

	mc.On("InsertInstance", "zone-1", mock.Anything).Return(
		nil, errors.New("err")).Once()

	_, err = gce.Boot([]db.Machine{{Size: "size1"}})
	assert.EqualError(t, err, "err")

	name := 0
	randName = func() string {
		name++
		return fmt.Sprintf("%d", name)
	}

	machines := []db.Machine{{Size: "size1"}, {Size: "size2"}}

	cfg1 := gce.instanceConfig("1", "size1", cfg.Ubuntu(machines[0], ""))
	mc.On("InsertInstance", "zone-1", cfg1).Return(nil, nil)

	cfg2 := gce.instanceConfig("2", "size2", cfg.Ubuntu(machines[1], ""))
	mc.On("InsertInstance", "zone-1", cfg2).Return(nil, nil)

	ids, err := gce.Boot(machines)
	assert.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, ids)

	mc.AssertExpectations(t)
}

func TestStop(t *testing.T) {
	mc, gce := getProvider()

	mc.On("DeleteInstance", mock.Anything, mock.Anything).Return(
		nil, errors.New("err")).Once()
	err := gce.Stop([]db.Machine{{CloudID: "1"}})
	assert.EqualError(t, err, "err")

	mc.On("DeleteInstance", "zone-1", "1").Return(nil, nil)
	mc.On("DeleteInstance", "zone-1", "2").Return(nil, nil)
	assert.NoError(t, gce.Stop([]db.Machine{{CloudID: "1"}, {CloudID: "2"}}))
	mc.AssertExpectations(t)
}

func TestInstanceConfig(t *testing.T) {
	_, gce := getProvider()
	cloudConfig := "cloudConfig"
	res := gce.instanceConfig("name", "size", cloudConfig)
	exp := &compute.Instance{
		Name:        "name",
		Description: gce.network,
		MachineType: "zones/zone-1/machineTypes/size",
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
			Network: gce.networkURL(),
		}},
		Metadata: &compute.Metadata{
			Items: []*compute.MetadataItems{{
				Key:   "startup-script",
				Value: &cloudConfig,
			}},
		},
		Tags: &compute.Tags{
			Items: []string{gce.zone},
		},
	}

	assert.Equal(t, exp, res)
}
