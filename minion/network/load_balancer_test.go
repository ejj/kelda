package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/minion/ovsdb/mocks"
)

func TestUpdateLoadBalancerIPs(t *testing.T) {
	client := new(mocks.Client)

	// Test error handling.
	client.On("ListLoadBalancers").Return(nil, assert.AnError).Once()
	updateLoadBalancerIPs(client, nil, nil)
	client.AssertNotCalled(t, "CreateLoadBalancer",
		mock.Anything, mock.Anything, mock.Anything)
	client.AssertNotCalled(t, "DeleteLoadBalancer", mock.Anything, mock.Anything)

	// Test joining load balancers.
	client.On("ListLoadBalancers").Return([]ovsdb.LoadBalancer{
		{
			Name: "red",
			VIPs: map[string]string{"10.0.0.2": "10.0.0.3,10.0.0.4"},
		},
		{
			Name: "bad",
		},
	}, nil).Once()
	client.On("DeleteLoadBalancer",
		lSwitch, ovsdb.LoadBalancer{Name: "bad"}).Return(nil)
	client.On("CreateLoadBalancer", lSwitch, "new",
		map[string]string{"10.0.0.10": "10.0.0.11"}).Return(nil)
	updateLoadBalancerIPs(client, []db.LoadBalancer{
		{
			Name:      "red",
			IP:        "10.0.0.2",
			Hostnames: []string{"red", "blue"},
		},
		{
			Name:      "new",
			IP:        "10.0.0.10",
			Hostnames: []string{"yellow"},
		},
	}, map[string]string{
		"red":    "10.0.0.4",
		"blue":   "10.0.0.3",
		"yellow": "10.0.0.11",
	})
	client.AssertExpectations(t)
}

func TestUpdateLoadBalancerARP(t *testing.T) {
	client := new(mocks.Client)

	// Test error handling.
	client.On("ListSwitchPort",
		mock.Anything).Return(ovsdb.SwitchPort{}, assert.AnError).Once()
	updateLoadBalancerARP(client, nil)
	client.AssertNotCalled(t, "UpdateSwitchPortAddresses",
		mock.Anything, mock.Anything)

	// Test properly replaces wrong ARP mapping.
	client.On("ListSwitchPort", mock.Anything).Return(ovsdb.SwitchPort{
		Addresses: []string{"10.0.0.2 " + ipdef.LoadBalancerMac},
	}, nil).Once()
	client.On("UpdateSwitchPortAddresses", loadBalancerSwitchPort,
		[]string{"10.0.0.2 10.0.0.3 " + ipdef.LoadBalancerMac}).Return(nil).Once()
	updateLoadBalancerARP(client, []db.LoadBalancer{
		{IP: "10.0.0.2"}, {IP: "10.0.0.3"},
	})
	client.AssertExpectations(t)

	// Test ignores order IP ordering.
	client.Calls = nil
	client.On("ListSwitchPort", mock.Anything).Return(ovsdb.SwitchPort{
		Addresses: []string{"10.0.0.2 10.0.0.3 " + ipdef.LoadBalancerMac},
	}, nil).Once()
	updateLoadBalancerARP(client, []db.LoadBalancer{
		{IP: "10.0.0.3"}, {IP: "10.0.0.2"},
	})
	client.AssertNotCalled(t, "UpdateSwitchPortAddresses",
		mock.Anything, mock.Anything)
}
