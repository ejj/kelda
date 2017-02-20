package google

import (
	"testing"

	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/stretchr/testify/suite"

	compute "google.golang.org/api/compute/v1"
)

type GoogleTestSuite struct {
	suite.Suite

	gce  *mockClient
	clst *Cluster
}

func (s *GoogleTestSuite) SetupTest() {
	s.gce = &mockClient{}
	s.clst = &Cluster{
		gce:    s.gce,
		projID: "project",
		ns:     "namespace",
		zone:   "zone-1",
	}
}

func (s *GoogleTestSuite) TestList() {
	s.gce.On("ListInstances", "project", "zone-1", apiOptions{
		filter: "description eq namespace",
	}).Return(&compute.InstanceList{
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

	machines, err := s.clst.List()
	s.NoError(err)
	s.Len(machines, 1)
	s.Equal(machines[0], machine.Machine{
		ID:        "name-1",
		PublicIP:  "x.x.x.x",
		PrivateIP: "y.y.y.y",
		Size:      "type-1",
		Region:    "zone-1",
		Provider:  db.Google,
	})
}

func TestGoogleTestSuite(t *testing.T) {
	suite.Run(t, new(GoogleTestSuite))
}
