//go:generate mockery -inpkg -name=client
package digitalocean

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalocean/godo"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"
)

const testNamespace = "namespace"
const errMsg = "error"

var errMock = errors.New(errMsg)

var network = &godo.Networks{
	V4: []godo.NetworkV4{
		{
			IPAddress: "privateIP",
			Netmask:   "255.255.255.255",
			Gateway:   "2.2.2.2",
			Type:      "private",
		},
		{
			IPAddress: "publicIP",
			Netmask:   "255.255.255.255",
			Gateway:   "2.2.2.2",
			Type:      "public",
		},
	},
}

var sfo = &godo.Region{
	Slug: DefaultRegion,
}

func init() {
	util.AppFs = afero.NewMemMapFs()
	keyFile := filepath.Join(os.Getenv("HOME"), apiKeyPath)
	util.WriteFile(keyFile, []byte("foo"), 0666)
}

func TestList(t *testing.T) {
	mc := new(mockClient)
	// Create a list of Droplets, that are paginated.
	dropFirst := []godo.Droplet{
		{
			ID:        123,
			Name:      testNamespace,
			Networks:  network,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    sfo,
		},

		// This droplet should not be listed because it has a name different from
		// testNamespace.
		{
			ID:        124,
			Name:      "foo",
			Networks:  network,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    sfo,
		},
	}

	dropLast := []godo.Droplet{
		{
			ID:        125,
			Name:      testNamespace,
			Networks:  network,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    sfo,
		},
	}

	respFirst := &godo.Response{
		Links: &godo.Links{
			Pages: &godo.Pages{
				Last: "2",
			},
		},
	}

	respLast := &godo.Response{
		Links: &godo.Links{},
	}

	reqFirst := &godo.ListOptions{}
	mc.On("ListDroplets", reqFirst).Return(dropFirst, respFirst, nil).Once()

	reqLast := &godo.ListOptions{
		Page: reqFirst.Page + 1,
	}
	mc.On("ListDroplets", reqLast).Return(dropLast, respLast, nil).Once()

	mc.On("GetVolume", mock.Anything).Return(
		&godo.Volume{
			SizeGigaBytes: 32,
		}, nil, nil,
	).Twice()

	doClust, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doClust.client = mc

	machines, err := doClust.List()
	assert.Nil(t, err)
	assert.Equal(t, machines, []machine.Machine{
		{
			ID:        "123",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			Region:    "sfo1",
			Reserved:  true,
		},
		{
			ID:        "125",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			Region:    "sfo1",
			Reserved:  true,
		},
	})

	// Error ListDroplets.
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, errMock).Once()
	machines, err = doClust.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, errMsg)

	// Error PublicIPv4. We can't error PrivateIPv4 because of the two functions'
	// similarities and the order that they are called in `List`.
	droplets := []godo.Droplet{
		{
			ID:        123,
			Name:      testNamespace,
			Networks:  nil,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    sfo,
		},
	}
	mc.On("ListDroplets", mock.Anything).Return(droplets, respLast, nil).Once()
	machines, err = doClust.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, "no networks have been defined")

	respBad := &godo.Response{
		Links: &godo.Links{
			Pages: &godo.Pages{
				Prev: "badurl",
				Last: "2",
			},
		},
	}
	mc.On("ListDroplets", mock.Anything).Return([]godo.Droplet{}, respBad, nil).Once()
	machines, err = doClust.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, "parse badurl: invalid URI for request")
}

func TestBoot(t *testing.T) {
	mc := new(mockClient)
	doClust, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doClust.client = mc

	util.Sleep = func(t time.Duration) {}

	bootSet := []machine.Machine{}
	err = doClust.Boot(bootSet)
	assert.Nil(t, err)

	// Create a list of machines to boot.
	bootSet = []machine.Machine{
		{
			ID:        "123",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
			Region:    "sfo1",
		},
	}

	mc.On("GetDroplet", 123).Return(&godo.Droplet{
		Status:    "active",
		VolumeIDs: []string{"abc"},
	}, nil, nil).Twice()

	mc.On("CreateDroplet", mock.Anything).Return(&godo.Droplet{
		ID: 123,
	}, nil, nil).Once()

	mc.On("CreateVolume", mock.Anything).Return(&godo.Volume{
		ID: "abc",
	}, nil, nil).Once()

	mc.On("AttachVolume", mock.Anything, mock.Anything).Return(nil, nil, nil).Once()

	err = doClust.Boot(bootSet)
	// Make sure machines are booted.
	mc.AssertNumberOfCalls(t, "CreateDroplet", 1)
	assert.Nil(t, err)

	// Error CreateDroplet.
	doubleBootSet := append(bootSet, machine.Machine{
		ID:        "123",
		Provider:  db.DigitalOcean,
		PublicIP:  "publicIP",
		PrivateIP: "privateIP",
		Size:      "size",
		DiskSize:  0,
		Region:    "sfo1",
	})
	mc.On("CreateDroplet", mock.Anything).Return(nil, nil, errMock).Twice()
	err = doClust.Boot(doubleBootSet)
	assert.EqualError(t, err, errMsg)
}

func TestStop(t *testing.T) {
	mc := new(mockClient)
	doClust, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doClust.client = mc

	util.Sleep = func(t time.Duration) {}

	// Test empty stop set
	stopSet := []machine.Machine{}
	err = doClust.Stop(stopSet)
	assert.Nil(t, err)

	// Test non-empty stop set
	stopSet = []machine.Machine{
		{
			ID:        "123",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
			Region:    "sfo1",
		},
	}

	mc.On("GetDroplet", 123).Return(&godo.Droplet{
		Status:    "active",
		VolumeIDs: []string{"abc"},
	}, nil, nil).Once()

	mc.On("GetDroplet", 123).Return(nil, nil, nil).Once()

	mc.On("DeleteDroplet", 123).Return(nil, nil).Once()

	mc.On("DeleteVolume", "abc").Return(nil, nil).Once()

	err = doClust.Stop(stopSet)

	// Make sure machines are stopped.
	mc.AssertNumberOfCalls(t, "GetDroplet", 2)
	assert.Nil(t, err)

	// Error strconv.
	badDoubleStopSet := []machine.Machine{
		{
			ID:        "123a",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
			Region:    "sfo1",
		},
		{
			ID:        "123a",
			Provider:  db.DigitalOcean,
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
			Region:    "sfo1",
		},
	}
	err = doClust.Stop(badDoubleStopSet)
	assert.Error(t, err)

	// Error DeleteDroplet.
	mc.On("GetDroplet", 123).Return(&godo.Droplet{
		Status:    "active",
		VolumeIDs: []string{"abc"},
	}, nil, nil).Once()

	mc.On("DeleteDroplet", 123).Return(nil, errMock).Once()
	err = doClust.Stop(stopSet)
	assert.EqualError(t, err, errMsg)
}

func TestSetACLs(t *testing.T) {
	doClust, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	err = doClust.SetACLs([]acl.ACL{
		{
			CidrIP:  "digital",
			MinPort: 1,
			MaxPort: 65535,
		},
		{
			CidrIP:  "ocean",
			MinPort: 22,
			MaxPort: 22,
		},
	})
	assert.NoError(t, err)
}

func TestUpdateFloatingIPs(t *testing.T) {
	doClust, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	err = doClust.UpdateFloatingIPs(nil)
	assert.EqualError(t, err, "digitalOcean floating IPs are unimplemented")
}

func TestNew(t *testing.T) {
	mc := new(mockClient)
	clust := &Cluster{
		namespace: testNamespace,
		client:    mc,
	}

	// Log a bad namespace.
	newDigitalOcean("___ILLEGAL---", DefaultRegion)

	// newDigitalOcean throws an error.
	newDigitalOcean = func(namespace, region string) (*Cluster, error) {
		return nil, errMock
	}
	outClust, err := New(testNamespace, DefaultRegion)
	assert.Nil(t, outClust)
	assert.EqualError(t, err, "error")

	// Normal operation.
	newDigitalOcean = func(namespace, region string) (*Cluster, error) {
		return clust, nil
	}
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, nil).Once()
	outClust, err = New(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	assert.Equal(t, clust, outClust)

	// ListDroplets throws an error.
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, errMock)
	outClust, err = New(testNamespace, DefaultRegion)
	assert.Equal(t, clust, outClust)
	assert.EqualError(t, err, errMsg)
}
