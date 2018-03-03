package digitalocean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/digitalocean/godo"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/cloud/digitalocean/client/mocks"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
)

const testNamespace = "namespace"
const testRegion = "region"
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

var godoRegion = &godo.Region{
	Slug: testRegion,
}

func init() {
	util.AppFs = afero.NewMemMapFs()
	keyFile := filepath.Join(os.Getenv("HOME"), apiKeyPath)
	util.WriteFile(keyFile, []byte("foo"), 0666)
	second = 0
}

func TestList(t *testing.T) {
	mc := new(mocks.Client)
	tag := fmt.Sprintf("%s-%s", testNamespace, testRegion)
	// Create a list of Droplets, that are paginated.
	dropFirst := []godo.Droplet{
		{
			ID:        123,
			Networks:  network,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    godoRegion,
			Tags:      []string{tag},
		}, {
			ID:       124,
			Networks: network,
			SizeSlug: "size",
			Region:   godoRegion,
			Tags:     []string{"wrong-region"},
		}}

	dropLast := []godo.Droplet{
		{
			ID:        125,
			Networks:  network,
			SizeSlug:  "size",
			VolumeIDs: []string{"foo"},
			Region:    godoRegion,
			Tags:      []string{tag},
		}}

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

	reqFirst := &godo.ListOptions{Page: 1, PerPage: 200}
	mc.On("ListDroplets", reqFirst).Return(
		dropFirst, respFirst, nil).Once()

	reqLast := &godo.ListOptions{
		Page:    reqFirst.Page + 1,
		PerPage: 200,
	}
	mc.On("ListDroplets", reqLast).Return(
		dropLast, respLast, nil).Once()

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

	doPrvdr, err := newDigitalOcean(testNamespace, testRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	machines, err := doPrvdr.List()
	assert.Nil(t, err)
	assert.Equal(t, []db.Machine{
		{
			Provider:    "DigitalOcean",
			Region:      testRegion,
			CloudID:     "123",
			PublicIP:    "publicIP",
			PrivateIP:   "privateIP",
			Size:        "size",
			Preemptible: false,
		},
		{
			Provider:    "DigitalOcean",
			Region:      testRegion,
			CloudID:     "125",
			PublicIP:    "publicIP",
			PrivateIP:   "privateIP",
			FloatingIP:  "floatingIP",
			Size:        "size",
			Preemptible: false}}, machines)

	// Error ListDroplets.
	mc.On("ListFloatingIPs", mock.Anything).Return(nil, &godo.Response{}, nil).Once()
	mc.On("ListDroplets", mock.Anything).Return(
		nil, nil, errMock).Once()
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
			Region:    godoRegion,
		},
	}
	mc.On("ListDroplets", mock.Anything).Return(
		droplets, respLast, nil).Once()
	mc.On("ListFloatingIPs", mock.Anything).Return(nil, &godo.Response{}, nil).Once()
	machines, err = doPrvdr.List()
	assert.Nil(t, machines)
	assert.EqualError(t, err, "get public IP: no networks have been defined")
}

func TestBoot(t *testing.T) {
	mc := new(mocks.Client)
	doPrvdr, err := newDigitalOcean(testNamespace, testRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	bootSet := []db.Machine{}
	ids, err := doPrvdr.Boot(bootSet)
	assert.Nil(t, err)
	assert.Nil(t, ids)

	// DigitalOcean limits the batch size to 10, so by booting 11 we exercise the
	// batch splitting code.
	for i := 0; i < 11; i++ {
		bootSet = append(bootSet, db.Machine{Size: "size1"})
	}
	bootSet = append(bootSet, db.Machine{Size: "size2"}, db.Machine{Size: "size2"})

	userData := cfg.Ubuntu(bootSet[0], "")
	mc.On("CreateDroplets", &godo.DropletMultiCreateRequest{
		Names: []string{"Kelda", "Kelda", "Kelda", "Kelda", "Kelda",
			"Kelda", "Kelda", "Kelda", "Kelda", "Kelda"},
		Region:            testRegion,
		Size:              "size1",
		Image:             godo.DropletCreateImage{ID: imageID},
		PrivateNetworking: true,
		UserData:          userData,
		Tags:              []string{doPrvdr.getTag()},
	}).Return([]godo.Droplet{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5},
		{ID: 6}, {ID: 7}, {ID: 8}, {ID: 9}, {ID: 10}}, nil, nil).Once()

	mc.On("CreateDroplets", &godo.DropletMultiCreateRequest{
		Names:             []string{"Kelda"},
		Region:            testRegion,
		Size:              "size1",
		Image:             godo.DropletCreateImage{ID: imageID},
		PrivateNetworking: true,
		UserData:          userData,
		Tags:              []string{doPrvdr.getTag()},
	}).Return([]godo.Droplet{{ID: 11}}, nil, nil).Once()

	mc.On("CreateDroplets", &godo.DropletMultiCreateRequest{
		Names:             []string{"Kelda", "Kelda"},
		Region:            testRegion,
		Size:              "size2",
		Image:             godo.DropletCreateImage{ID: imageID},
		PrivateNetworking: true,
		UserData:          userData,
		Tags:              []string{doPrvdr.getTag()},
	}).Return([]godo.Droplet{{ID: 12}, {ID: 13}}, nil, nil).Once()

	ids, err = doPrvdr.Boot(bootSet)
	mc.AssertExpectations(t)
	assert.Nil(t, err)
	sort.Strings(ids)
	assert.Equal(t, []string{"1", "10", "11", "12", "13", "2", "3", "4",
		"5", "6", "7", "8", "9"}, ids)

	mc.On("CreateDroplets", mock.Anything).Return(nil, nil, errMock)
	ids, err = doPrvdr.Boot(bootSet)
	assert.EqualError(t, err, errMsg)
	assert.Nil(t, ids)
}

func TestBootPreemptible(t *testing.T) {
	t.Parallel()

	ids, err := Provider{}.Boot([]db.Machine{{Preemptible: true}})
	assert.EqualError(t, err, "preemptible instances are not implemented")
	assert.Nil(t, ids)
}

func TestStop(t *testing.T) {
	mc := new(mocks.Client)
	doPrvdr, err := newDigitalOcean(testNamespace, testRegion)
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

	mc.On("DeleteDroplet", 123).Return(nil, nil).Once()

	mc.On("DeleteVolume", "abc").Return(nil, nil).Once()

	err = doPrvdr.Stop(stopSet)
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
	mc.On("DeleteDroplet", 123).Return(nil, errMock).Once()
	err = doPrvdr.Stop(stopSet)
	assert.EqualError(t, err, errMsg)
}

func TestSetACLs(t *testing.T) {
	mc := new(mocks.Client)
	doPrvdr, err := newDigitalOcean(testNamespace, testRegion)
	assert.Nil(t, err)
	doPrvdr.Client = mc

	tagName := testNamespace + "-" + testRegion
	acls := []acl.ACL{
		{
			CidrIP:  "10.0.0.0/24",
			MinPort: 1,
			MaxPort: 65535,
		},
		{
			CidrIP:  "11.0.0.0/27",
			MinPort: 22,
			MaxPort: 22,
		},
	}

	internalDroplets := &godo.Sources{Tags: []string{tagName}}
	internalTrafficRules := []godo.InboundRule{
		{
			Protocol:  "tcp",
			PortRange: "all",
			Sources:   internalDroplets,
		},
		{
			Protocol:  "udp",
			PortRange: "all",
			Sources:   internalDroplets,
		},
	}

	// Test that creating new ACLs works as expected.
	mc.On("CreateTag", tagName).Return(
		&godo.Tag{Name: tagName}, nil, nil).Once()
	mc.On("CreateFirewall", tagName, allowAll, nil).Return(
		&godo.Firewall{ID: "test", OutboundRules: allowAll}, nil, nil).Once()
	mc.On("ListFirewalls", mock.Anything).Return(
		[]godo.Firewall{
			{Tags: []string{tagName}, ID: "test", OutboundRules: allowAll},
		}, nil, nil).Once()
	mc.On("AddRules", "test",
		mock.AnythingOfType("[]godo.InboundRule")).Return(nil, nil).Once()
	mc.On("RemoveRules", "test", []godo.InboundRule(nil)).Return(nil, nil).Once()

	err = doPrvdr.SetACLs(acls)
	assert.NoError(t, err)
	checkFirewallRuleCall(t, mc, "AddRules",
		append(toRules(acls), internalTrafficRules...))

	mc = new(mocks.Client)
	doPrvdr.Client = mc

	// Check that ACLs are both created and removed when not in the requested list.
	extraRules := append(toRules([]acl.ACL{
		{
			CidrIP:  "12.0.0.0/29",
			MinPort: 100,
			MaxPort: 200,
		}}),
		godo.InboundRule{Sources: &godo.Sources{Tags: []string{"random-tag"}}})
	mc.On("ListFirewalls", mock.Anything).Return([]godo.Firewall{
		{
			Tags:          []string{tagName},
			ID:            "test",
			OutboundRules: allowAll,
			InboundRules: append(internalTrafficRules,
				append(toRules([]acl.ACL{acls[1]}), extraRules...)...),
		},
	}, nil, nil).Once()

	mc.On("AddRules", "test", mock.Anything).Return(nil, nil).Once()
	mc.On("RemoveRules", "test", mock.Anything).Return(nil, nil).Once()

	err = doPrvdr.SetACLs(acls)
	assert.NoError(t, err)
	checkFirewallRuleCall(t, mc, "AddRules", toRules([]acl.ACL{acls[0]}))
	checkFirewallRuleCall(t, mc, "RemoveRules", extraRules)

	mc = new(mocks.Client)
	doPrvdr.Client = mc

	// Check that SetACLs fails on error.
	mc.On("CreateTag", tagName).Return(
		&godo.Tag{Name: tagName}, nil, nil).Once()
	mc.On("CreateFirewall", tagName, allowAll,
		mock.AnythingOfType("[]godo.InboundRule")).Return(
		&godo.Firewall{ID: "test", OutboundRules: allowAll}, nil, nil).Once()
	mc.On("ListFirewalls", mock.Anything).Return(
		[]godo.Firewall{
			{Tags: []string{tagName}, ID: "test", OutboundRules: allowAll},
		}, nil, nil).Once()
	mc.On("AddRules", "test",
		mock.AnythingOfType("[]godo.InboundRule")).Return(nil, errMock).Once()
	mc.On("RemoveRules", "test",
		mock.AnythingOfType("[]godo.InboundRule")).Return(nil, nil).Once()

	err = doPrvdr.SetACLs(acls)
	assert.Error(t, err)

	// Check that getCreateFirewall checks all pages of firewalls.
	fwFirst := []godo.Firewall{{Tags: []string{"otherFW"}, ID: "testWrong"}}
	fwLast := []godo.Firewall{{Tags: []string{tagName}, ID: "testCorrect"}}

	respFirst := &godo.Response{
		Links: &godo.Links{Pages: &godo.Pages{Last: "2"}},
	}
	respLast := &godo.Response{Links: &godo.Links{}}

	reqFirst := &godo.ListOptions{Page: 1, PerPage: 200}
	mc.On("ListFirewalls", reqFirst).Return(fwFirst, respFirst, nil).Once()

	reqLast := &godo.ListOptions{
		Page:    reqFirst.Page + 1,
		PerPage: 200,
	}
	mc.On("ListFirewalls", reqLast).Return(fwLast, respLast, nil).Once()
	mc.On("CreateFirewall", mock.Anything, mock.Anything, mock.Anything).Return(
		nil, errMock).Once()

	fw, err := doPrvdr.getCreateFirewall()
	assert.NoError(t, err)
	assert.Equal(t, "testCorrect", fw.ID)
}

// checkFirewallRuleCall asserts that the given firewall API call was called
// with the given rules. It ignores the order of the rules.
func checkFirewallRuleCall(t *testing.T, mc *mocks.Client, method string,
	expRules []godo.InboundRule) {
	call, err := getCall(mc, method)
	if err != nil {
		t.Error(err)
		return
	}

	assert.Len(t, call.Arguments, 2)
	actualRules := call.Arguments[1].([]godo.InboundRule)
	assert.Len(t, actualRules, len(expRules))
	assert.Subset(t, actualRules, expRules)
}

func getCall(mc *mocks.Client, method string) (mock.Call, error) {
	var desiredCall mock.Call
	var foundCall bool
	for _, call := range mc.Calls {
		if call.Method == method {
			if foundCall {
				return mock.Call{}, fmt.Errorf(
					"%q called multiple times", method)
			}
			foundCall = true
			desiredCall = call
		}
	}

	if !foundCall {
		return mock.Call{}, fmt.Errorf("failed to find %q call", method)
	}
	return desiredCall, nil
}

func TestToRules(t *testing.T) {
	// Test converting ACLs with a variety of port ranges.
	acls := []acl.ACL{
		{CidrIP: "1.0.0.0/8", MinPort: 80, MaxPort: 100},
		{CidrIP: "2.0.0.0/8", MinPort: 80, MaxPort: 80},
		{CidrIP: "3.0.0.0/8", MinPort: 0, MaxPort: 100},
		{CidrIP: "1.0.0.0/8", MinPort: 4000, MaxPort: 4000},
		{CidrIP: "1.0.0.0/8", MinPort: 500, MaxPort: 600},
	}
	srcForIP := func(ip string) *godo.Sources {
		return &godo.Sources{Addresses: []string{ip}}
	}
	godoRules := []godo.InboundRule{
		{Protocol: "tcp", PortRange: "80-100", Sources: srcForIP("1.0.0.0/8")},
		{Protocol: "udp", PortRange: "80-100", Sources: srcForIP("1.0.0.0/8")},
		{Protocol: "icmp", Sources: srcForIP("1.0.0.0/8")},

		{Protocol: "tcp", PortRange: "80", Sources: srcForIP("2.0.0.0/8")},
		{Protocol: "udp", PortRange: "80", Sources: srcForIP("2.0.0.0/8")},
		{Protocol: "icmp", Sources: srcForIP("2.0.0.0/8")},

		{Protocol: "tcp", PortRange: "0-100", Sources: srcForIP("3.0.0.0/8")},
		{Protocol: "udp", PortRange: "0-100", Sources: srcForIP("3.0.0.0/8")},
		{Protocol: "icmp", Sources: srcForIP("3.0.0.0/8")},

		{Protocol: "tcp", PortRange: "4000", Sources: srcForIP("1.0.0.0/8")},
		{Protocol: "udp", PortRange: "4000", Sources: srcForIP("1.0.0.0/8")},
		// We do not want a duplicate ICMP rule here.

		{Protocol: "tcp", PortRange: "500-600", Sources: srcForIP("1.0.0.0/8")},
		{Protocol: "udp", PortRange: "500-600", Sources: srcForIP("1.0.0.0/8")},
		// Nor do we want one here.
	}
	assert.Equal(t, godoRules, toRules(acls))
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
	newDigitalOcean("___ILLEGAL---", testRegion)

	// newDigitalOcean throws an error.
	newDigitalOcean = func(namespace, region string) (*Provider, error) {
		return nil, errMock
	}
	outClient, err := New(testNamespace, testRegion)
	assert.Nil(t, outClient)
	assert.EqualError(t, err, "error")

	// Normal operation.
	newDigitalOcean = func(namespace, region string) (*Provider, error) {
		return client, nil
	}
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, nil).Once()
	outClient, err = New(testNamespace, testRegion)
	assert.Nil(t, err)
	assert.Equal(t, client, outClient)

	// ListDroplets throws an error.
	mc.On("ListDroplets", mock.Anything).Return(nil, nil, errMock)
	outClient, err = New(testNamespace, testRegion)
	assert.Equal(t, client, outClient)
	assert.EqualError(t, err, errMsg)
}

func TestCleanup(t *testing.T) {
	mc := new(mocks.Client)
	client := &Provider{Client: mc}

	mc.On("ListFirewalls", mock.Anything).Return(nil, nil, assert.AnError).Once()
	assert.Error(t, client.Cleanup())

	mc.On("ListFirewalls", mock.Anything).Return(
		[]godo.Firewall{{Tags: []string{client.getTag()}, ID: "test"}}, nil, nil)
	mc.On("DeleteFirewall", "test").Return(nil, assert.AnError).Once()
	assert.Error(t, client.Cleanup())

	mc.On("DeleteFirewall", "test").Return(nil, nil)
	assert.NoError(t, client.Cleanup())
}
