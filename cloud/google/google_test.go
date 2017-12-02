package google

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/google/client/mocks"
	"github.com/kelda/kelda/db"
	"github.com/stretchr/testify/suite"

	compute "google.golang.org/api/compute/v1"
)

type GoogleTestSuite struct {
	suite.Suite

	gce *mocks.Client
	*Provider
}

func (s *GoogleTestSuite) SetupTest() {
	s.gce = new(mocks.Client)
	s.Provider = &Provider{
		Client:    s.gce,
		namespace: "namespace",
		network:   "network",
		zone:      "zone-1",
	}
}

func (s *GoogleTestSuite) TestList() {
	s.gce.On("ListInstances", "zone-1",
		"description eq namespace").Return(&compute.InstanceList{
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

	machines, err := s.List()
	s.NoError(err)
	s.Len(machines, 1)
	s.Equal(machines[0], db.Machine{
		Provider:  "Google",
		Region:    "zone-1",
		CloudID:   "name-1",
		PublicIP:  "x.x.x.x",
		PrivateIP: "y.y.y.y",
		Size:      "type-1",
	})
}

func (s *GoogleTestSuite) TestListFirewalls() {
	s.network = "network"
	s.intFW = "intFW"

	s.gce.On("ListFirewalls").Return(&compute.FirewallList{
		Items: []*compute.Firewall{
			{
				Network:    s.networkURL(),
				Name:       "badZone",
				TargetTags: []string{"zone-2"},
			},
			{
				Network:    s.networkURL(),
				Name:       "intFW",
				TargetTags: []string{"zone-1"},
			},
			{
				Network:    "ignoreMe",
				Name:       "badNetwork",
				TargetTags: []string{"zone-1"},
			},
			{
				Network:    s.networkURL(),
				Name:       "shouldReturn",
				TargetTags: []string{"zone-1"},
			},
		},
	}, nil).Once()

	fws, err := s.listFirewalls()
	s.NoError(err)
	s.Len(fws, 1)
	s.Equal(fws[0].Name, "shouldReturn")

	s.gce.On("ListFirewalls").Return(nil, errors.New("err")).Once()
	_, err = s.listFirewalls()
	s.EqualError(err, "list firewalls: err")
}

func (s *GoogleTestSuite) TestListBadNetworkInterface() {
	// Tests that List returns an error when no network interfaces are
	// configured.
	s.gce.On("ListInstances", "zone-1",
		"description eq namespace").Return(&compute.InstanceList{
		Items: []*compute.Instance{
			{
				MachineType:       "machine/split/type-1",
				Name:              "name-1",
				NetworkInterfaces: []*compute.NetworkInterface{},
			},
		},
	}, nil)

	_, err := s.List()
	s.EqualError(err, "Google instances are expected to have exactly 1 "+
		"interface; for instance name-1, found 0")
}

func (s *GoogleTestSuite) TestParseACLs() {
	parsed, err := s.parseACLs([]compute.Firewall{
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
	s.NoError(err)
	s.Equal([]acl.ACL{
		{MinPort: 80, MaxPort: 80, CidrIP: "foo"},
		{MinPort: 20, MaxPort: 25, CidrIP: "foo"},
		{MinPort: 80, MaxPort: 80, CidrIP: "bar"},
		{MinPort: 20, MaxPort: 25, CidrIP: "bar"},
		{MinPort: 1, MaxPort: 65535, CidrIP: "foo"},
	}, parsed)

	_, err = s.parseACLs([]compute.Firewall{
		{
			Name: "firewall",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"NaN"}},
			},
			SourceRanges: []string{"foo"},
		},
	})
	s.EqualError(err, `parse ports of firewall: parse ints: strconv.Atoi: `+
		`parsing "NaN": invalid syntax`)

	_, err = s.parseACLs([]compute.Firewall{
		{
			Name: "firewall",
			Allowed: []*compute.FirewallAllowed{
				{Ports: []string{"1-80-81"}},
			},
			SourceRanges: []string{"foo"},
		},
	})
	s.EqualError(err, "parse ports of firewall: unrecognized port format: 1-80-81")
}

func TestGoogleTestSuite(t *testing.T) {
	suite.Run(t, new(GoogleTestSuite))
}
