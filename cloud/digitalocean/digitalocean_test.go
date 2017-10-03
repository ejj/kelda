package digitalocean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalocean/godo"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/digitalocean/client/mocks"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
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
	mc := new(mocks.Client)
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

	floatingIPsFirst := []godo.FloatingIP{
		{IP: "ignored"},
		{Droplet: &godo.Droplet{ID: -1}, IP: "ignored"},
	}
	mc.On("ListFloatingIPs", reqFirst).Return(floatingIPsFirst, respFirst, nil).Once()

	floatingIPsLast := []godo.FloatingIP{
		{Droplet: &godo.Droplet{ID: 125}, IP: "floatingIP"},
	}
	mc.On("ListFloatingIPs", reqLast).Return(floatingIPsLast, respLast, nil).Once()

	mc.On("GetVolume", mock.Anything).Return(
		&godo.Volume{
			SizeGigaBytes: 32,
		}, nil, nil,
	).Twice()

	doPrvdr, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	machines, err := doPrvdr.List()
	assert.Nil(t, err)
	assert.Equal(t, machines, []db.Machine{
		{
			Provider:    "DigitalOcean",
			Region:      "sfo1",
			CloudID:     "123",
			PublicIP:    "publicIP",
			PrivateIP:   "privateIP",
			Size:        "size",
			Preemptible: false,
		},
		{
			Provider:    "DigitalOcean",
			Region:      "sfo1",
			CloudID:     "125",
			PublicIP:    "publicIP",
			PrivateIP:   "privateIP",
			FloatingIP:  "floatingIP",
			Size:        "size",
			Preemptible: false,
		},
	})

	// Error ListDroplets.
	mc.On("ListFloatingIPs", mock.Anything).Return(nil, &godo.Response{}, nil).Once()
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, errMock).Once()
	machines, err = doPrvdr.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, fmt.Sprintf("list droplets: %s", errMsg))

	// Error ListFloatingIPs.
	mc.On("ListFloatingIPs", mock.Anything).Return(nil, nil, errMock).Once()
	_, err = doPrvdr.List()
	assert.EqualError(t, err, fmt.Sprintf("list floating IPs: %s", errMsg))

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
	mc.On("ListFloatingIPs", mock.Anything).Return(nil, &godo.Response{}, nil).Once()
	machines, err = doPrvdr.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, "get public IP: no networks have been defined")
}

func TestBoot(t *testing.T) {
	mc := new(mocks.Client)
	doPrvdr, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	util.Sleep = func(t time.Duration) {}

	bootSet := []db.Machine{}
	err = doPrvdr.Boot(bootSet)
	assert.Nil(t, err)

	// Create a list of machines to boot.
	bootSet = []db.Machine{
		{
			CloudID:   "123",
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
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

	err = doPrvdr.Boot(bootSet)
	// Make sure machines are booted.
	mc.AssertNumberOfCalls(t, "CreateDroplet", 1)
	assert.Nil(t, err)

	// Error CreateDroplet.
	doubleBootSet := append(bootSet, db.Machine{
		CloudID:   "123",
		PublicIP:  "publicIP",
		PrivateIP: "privateIP",
		Size:      "size",
		DiskSize:  0,
	})
	mc.On("CreateDroplet", mock.Anything).Return(nil, nil, errMock).Twice()
	err = doPrvdr.Boot(doubleBootSet)
	assert.EqualError(t, err, errMsg)
}

func TestBootPreemptible(t *testing.T) {
	t.Parallel()

	err := Provider{}.Boot([]db.Machine{{Preemptible: true}})
	assert.EqualError(t, err, "preemptible instances are not yet implemented")
}

func TestStop(t *testing.T) {
	mc := new(mocks.Client)
	doPrvdr, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	util.Sleep = func(t time.Duration) {}

	// Test empty stop set
	stopSet := []db.Machine{}
	err = doPrvdr.Stop(stopSet)
	assert.Nil(t, err)

	// Test non-empty stop set
	stopSet = []db.Machine{
		{
			CloudID:   "123",
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
		},
	}

	mc.On("GetDroplet", 123).Return(&godo.Droplet{
		Status:    "active",
		VolumeIDs: []string{"abc"},
	}, nil, nil).Once()

	mc.On("GetDroplet", 123).Return(nil, nil, nil).Once()

	mc.On("DeleteDroplet", 123).Return(nil, nil).Once()

	mc.On("DeleteVolume", "abc").Return(nil, nil).Once()

	err = doPrvdr.Stop(stopSet)

	// Make sure machines are stopped.
	mc.AssertNumberOfCalls(t, "GetDroplet", 2)
	assert.Nil(t, err)

	// Error strconv.
	badDoubleStopSet := []db.Machine{
		{
			CloudID:   "123a",
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
		},
		{
			CloudID:   "123a",
			PublicIP:  "publicIP",
			PrivateIP: "privateIP",
			Size:      "size",
			DiskSize:  0,
		},
	}
	err = doPrvdr.Stop(badDoubleStopSet)
	assert.Error(t, err)

	// Error DeleteDroplet.
	mc.On("GetDroplet", 123).Return(&godo.Droplet{
		Status:    "active",
		VolumeIDs: []string{"abc"},
	}, nil, nil).Once()

	mc.On("DeleteDroplet", 123).Return(nil, errMock).Once()
	err = doPrvdr.Stop(stopSet)
	assert.EqualError(t, err, errMsg)
}

func TestSetACLs(t *testing.T) {
	doPrvdr, err := newDigitalOcean(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	err = doPrvdr.SetACLs([]acl.ACL{
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
	mc := new(mocks.Client)
	client := &Provider{Client: mc}

	mc.On("ListFloatingIPs", mock.Anything).Return(nil, nil, errMock).Once()
	err := client.UpdateFloatingIPs(nil)
	assert.EqualError(t, err,
		fmt.Sprintf("list machines: list floating IPs: %s", errMsg))
	mc.AssertExpectations(t)

	// Test assigning a floating IP.
	mc.On("AssignFloatingIP", "ip", 1).Return(nil, nil, nil).Once()
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "1"},
			{CloudID: "2"},
		},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
		},
	)
	assert.NoError(t, err)
	mc.AssertExpectations(t)

	// Test error when assigning a floating IP.
	mc.On("AssignFloatingIP", "ip", 1).Return(nil, nil, errMock).Once()
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "1"},
		},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
		},
	)
	assert.EqualError(t, err, fmt.Sprintf("assign IP (ip to 1): %s", errMsg))
	mc.AssertExpectations(t)

	// Test assigning one floating IP, and unassigning another.
	mc.On("AssignFloatingIP", "ip", 1).Return(nil, nil, nil).Once()
	mc.On("UnassignFloatingIP", "remove").Return(nil, nil, nil).Once()
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "1"},
			{CloudID: "2", FloatingIP: "remove"},
		},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
			{CloudID: "2"},
		},
	)
	assert.NoError(t, err)
	mc.AssertExpectations(t)

	// Test error when unassigning a floating IP.
	mc.On("UnassignFloatingIP", "remove").Return(nil, nil, errMock).Once()
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "2", FloatingIP: "remove"},
		},
		[]db.Machine{
			{CloudID: "2"},
		},
	)
	assert.EqualError(t, err, fmt.Sprintf("unassign IP (remove): %s", errMsg))
	mc.AssertExpectations(t)

	// Test changing a floating IP, which requires removing the old one, and
	// assigning the new.
	mc.On("UnassignFloatingIP", "changeme").Return(nil, nil, nil).Once()
	mc.On("AssignFloatingIP", "ip", 1).Return(nil, nil, nil).Once()
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "1", FloatingIP: "changeme"},
		},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
		},
	)
	assert.NoError(t, err)
	mc.AssertExpectations(t)

	// Test machines that need no changes.
	err = client.syncFloatingIPs(
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
		},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
		},
	)
	assert.NoError(t, err)
	mc.AssertExpectations(t)

	err = client.syncFloatingIPs(
		[]db.Machine{},
		[]db.Machine{
			{CloudID: "1", FloatingIP: "ip"},
			{CloudID: "2", FloatingIP: "ip2"},
		},
	)
	assert.EqualError(t, err, "no matching IDs: 1, 2")

	err = client.syncFloatingIPs(
		[]db.Machine{{CloudID: "NAN"}},
		[]db.Machine{
			{CloudID: "NAN", FloatingIP: "ip"},
		},
	)
	assert.EqualError(t, err,
		"malformed id (NAN): strconv.Atoi: parsing \"NAN\": invalid syntax")
}

func TestNew(t *testing.T) {
	mc := new(mocks.Client)
	client := &Provider{
		namespace: testNamespace,
		Client:    mc,
	}

	// Log a bad namespace.
	newDigitalOcean("___ILLEGAL---", DefaultRegion)

	// newDigitalOcean throws an error.
	newDigitalOcean = func(namespace, region string) (*Provider, error) {
		return nil, errMock
	}
	outClient, err := New(testNamespace, DefaultRegion)
	assert.Nil(t, outClient)
	assert.EqualError(t, err, "error")

	// Normal operation.
	newDigitalOcean = func(namespace, region string) (*Provider, error) {
		return client, nil
	}
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, nil).Once()
	outClient, err = New(testNamespace, DefaultRegion)
	assert.Nil(t, err)
	assert.Equal(t, client, outClient)

	// ListDroplets throws an error.
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, errMock)
	outClient, err = New(testNamespace, DefaultRegion)
	assert.Equal(t, client, outClient)
	assert.EqualError(t, err, errMsg)
}
