package network

import (
	"sort"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/util"
)

/*
Load balancing works by rewriting load balanced IPs using the Load_Balancer
table in the Logical_Switch and routing load balanced requests to the load balancer
router, which then forwards the rewritten packet to its new destination. Rewritten
packets are routed to the load balancer router because we setup the router's
logical switch port to respond to ARP requests for load balanced IPs with the
load balancer router's MAC.

For example, let's consider a load balanced IP 10.1.0.1 that load balances IPs
10.2.0.1 and 10.2.0.2. If a container makes a request to 10.1.0.1, the following
would happen:

1. The container issues an ARP request for 10.1.0.1.

2. The logical switch generates an ARP response containing the MAC address
of the load balancer router.

3. The container sends a packet destined for IP 10.1.0.1, and the load
balancer router's MAC.

4. The logical switch matches 10.1.0.1 to a load balancer IP, and rewrites the
IP address to a container IP (let's say 10.2.0.1).

5. The packet continues to the load balancer router because the MAC address was
unchanged.

6. The router receives a packet with its MAC address, but the IP address 10.2.0.1.

7. The router forwards the packet with the correct MAC address.

8. 10.2.0.1 receives the packet from the router.
*/
func updateLoadBalancers(client ovsdb.Client, loadBalancers []db.LoadBalancer,
	hostnameToIP map[string]string) {
	updateLoadBalancerIPs(client, loadBalancers, hostnameToIP)
	updateLoadBalancerARP(client, loadBalancers)
}

func updateLoadBalancerIPs(client ovsdb.Client, loadBalancers []db.LoadBalancer,
	hostnameToIP map[string]string) {
	curr, err := client.ListLoadBalancers()
	if err != nil {
		log.WithError(err).Error("Failed to get load balancers")
		return
	}

	var target []ovsdb.LoadBalancer
	for _, lb := range loadBalancers {
		var ips []string
		for _, hostname := range lb.Hostnames {
			ip := hostnameToIP[hostname]
			if ip != "" {
				ips = append(ips, ip)
			}
		}
		// Ignore the ip order.
		sort.Strings(ips)

		target = append(target, ovsdb.LoadBalancer{
			Name: lb.Name,
			VIPs: map[string]string{
				lb.IP: strings.Join(ips, ","),
			},
		})
	}

	key := func(intf interface{}) interface{} {
		lb := intf.(ovsdb.LoadBalancer)
		return struct{ Name, VIPs string }{
			Name: lb.Name,
			VIPs: util.MapAsString(lb.VIPs),
		}
	}
	_, toAdd, toRemove := join.HashJoin(loadBalancerSlice(target),
		loadBalancerSlice(curr), key, key)

	for _, intf := range toAdd {
		lb := intf.(ovsdb.LoadBalancer)
		err := client.CreateLoadBalancer(lSwitch, lb.Name, lb.VIPs)
		if err != nil {
			log.WithError(err).Error("Failed to create load balancer")
		} else {
			log.WithField("name", lb.Name).Debug("Created load balancer")
		}
	}

	for _, intf := range toRemove {
		lb := intf.(ovsdb.LoadBalancer)
		if err := client.DeleteLoadBalancer(lSwitch, lb); err != nil {
			log.WithError(err).Error("Failed to remove load balancer")
		} else {
			log.WithField("name", lb.Name).Debug("Removed load balancer")
		}
	}
}

// updateLoadBalancerARP updates the `addresses` field of the logical switch
// port attached to the load balancer router. This is necessary so that the
// switch port synthesizes ARP responses to load balanced VIPs.
func updateLoadBalancerARP(client ovsdb.Client, loadBalancers []db.LoadBalancer) {
	curr, err := client.ListSwitchPort(loadBalancerSwitchPort)
	if err != nil {
		log.WithError(err).Error("Failed to get load balancer switch port")
		return
	}

	var loadBalancedIPs []string
	for _, lb := range loadBalancers {
		loadBalancedIPs = append(loadBalancedIPs, lb.IP)
	}
	// Ignore the order of `loadBalancers`.
	sort.Strings(loadBalancedIPs)
	expAddresses := strings.Join(append(loadBalancedIPs, ipdef.LoadBalancerMac), " ")

	if len(curr.Addresses) != 1 || curr.Addresses[0] != expAddresses {
		err := client.UpdateSwitchPortAddresses(
			loadBalancerSwitchPort, []string{expAddresses})
		if err != nil {
			log.WithError(err).Error(
				"Failed to update load balancer switch port")
		}
	}
}

// loadBalancerSlice is an alias for []LoadBalancer to allow for joins
type loadBalancerSlice []ovsdb.LoadBalancer

// Get returns the value contained at the given index
func (lbs loadBalancerSlice) Get(i int) interface{} {
	return lbs[i]
}

// Len returns the number of items in the slice
func (lbs loadBalancerSlice) Len() int {
	return len(lbs)
}
