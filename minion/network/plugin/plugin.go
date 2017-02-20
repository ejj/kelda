package plugin

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/quilt/quilt/minion/ipdef"
	"github.com/quilt/quilt/minion/network/openflow"

	dnet "github.com/docker/go-plugins-helpers/network"
	"github.com/vishvananda/netlink"

	log "github.com/Sirupsen/logrus"
)

const (
	// NetworkName is the name of the network driver plugin.
	NetworkName = "quilt"

	pluginDir   = "/run/docker/plugins"
	ifacePrefix = "eth"
)

var (
	networkSocket = NetworkName + ".sock"
	pluginSocket  = filepath.Join(pluginDir, networkSocket)
)

type driver struct{}

const mtu int = 1400

// Run runs the network driver and starts the server to listen for requests. It will
// block until the server socket has been created.
func Run() {
	h := dnet.NewHandler(driver{})

	go ofctlRun()
	go vsctlRun()

	go func() {
		err := h.ServeUnix("root", pluginSocket)
		if err != nil {
			// If the driver fails to start, we can't boot any containers,
			// so we may as well panic.
			panic("Failed to serve driver socket server")
		}
	}()

	// The ServeUnix function that handles the plugin socket won't return until
	// the socket is closed, so we can't know exactly when the socket has been
	// created. In order to prevent a race condition where Docker attempts to use
	// the plugin before the socket is up, we simply wait until the socket file
	// exists.
	for {
		if _, err := os.Stat(pluginSocket); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// GetCapabilities returns the capabilities of this network driver.
func (d driver) GetCapabilities() (*dnet.CapabilitiesResponse, error) {
	return &dnet.CapabilitiesResponse{Scope: dnet.LocalScope}, nil
}

// CreateEndpoint acknowledges the request, but does not actually do anything.
func (d driver) CreateEndpoint(req *dnet.CreateEndpointRequest) (
	*dnet.CreateEndpointResponse, error) {

	addr, _, err := net.ParseCIDR(req.Interface.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid IP: %s", req.Interface.Address)
	}

	outer := ipdef.IFName(req.EndpointID)
	inner := ipdef.IFName("tmp_" + req.EndpointID)
	err = linkAdd(&netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: outer, MTU: mtu},
		PeerName:  inner})
	if err != nil {
		return nil, fmt.Errorf("failed to create veth: %s", err)
	}

	outerLink, err := getOuterLink(req.EndpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to find link %s: %s", outer, err)
	}

	if err := linkSetUp(outerLink); err != nil {
		return nil, fmt.Errorf("failed to bring up link %s: %s", outer, err)
	}

	mac := ipdef.IPToMac(addr)
	peerBr, peerQuilt := ipdef.PatchPorts(req.EndpointID)

	err = vsctl([][]string{
		{"add-port", ipdef.QuiltBridge, ipdef.IFName(req.EndpointID)},
		{"add-port", ipdef.QuiltBridge, peerQuilt},
		{"set", "Interface", peerQuilt, "type=patch", "options:peer=" + peerBr},
		{"add-port", ipdef.OvnBridge, peerBr},
		{"set", "Interface", peerBr, "type=patch", "options:peer=" + peerQuilt,
			"external-ids:attached-mac=" + mac,
			"external-ids:iface-id=" + addr.String()}})
	if err != nil {
		return nil, fmt.Errorf("ovs-vsctl: %v", err)
	}

	err = ofctl(openflow.Container{Veth: outer, Patch: peerQuilt, Mac: mac})
	if err != nil {
		// Problems with OpenFlow can be repaired later so just log.
		log.WithError(err).Warn("Failed to add OpenFlow rules")
	}

	resp := &dnet.CreateEndpointResponse{
		Interface: &dnet.EndpointInterface{
			MacAddress: mac,
		},
	}
	return resp, nil
}

// EndpointInfo will return an error if the endpoint does not exist.
func (d driver) EndpointInfo(req *dnet.InfoRequest) (*dnet.InfoResponse, error) {
	if _, err := getOuterLink(req.EndpointID); err != nil {
		return nil, err
	}
	return &dnet.InfoResponse{}, nil
}

// DeleteEndpoint cleans up state associated with a docker endpoint.
func (d driver) DeleteEndpoint(req *dnet.DeleteEndpointRequest) error {
	peerBr, peerQuilt := ipdef.PatchPorts(req.EndpointID)
	err := vsctl([][]string{
		{"del-port", ipdef.QuiltBridge, ipdef.IFName(req.EndpointID)},
		{"del-port", ipdef.QuiltBridge, peerQuilt},
		{"del-port", ipdef.OvnBridge, peerBr}})
	if err != nil {
		return fmt.Errorf("ovs-vsctl: %v", err)
	}

	outer, err := getOuterLink(req.EndpointID)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %s", req.EndpointID, err)
	}

	if err := linkDel(outer); err != nil {
		return fmt.Errorf("failed to delete link %s: %s", req.EndpointID, err)
	}

	return nil
}

// Join method is invoked when a Sandbox is attached to an endpoint.
func (d driver) Join(req *dnet.JoinRequest) (*dnet.JoinResponse, error) {
	inner := ipdef.IFName("tmp_" + req.EndpointID)
	resp := &dnet.JoinResponse{}
	resp.Gateway = ipdef.GatewayIP.String()
	resp.InterfaceName = dnet.InterfaceName{SrcName: inner, DstPrefix: ifacePrefix}
	return resp, nil
}

func getOuterLink(eid string) (netlink.Link, error) {
	return linkByName(ipdef.IFName(eid))
}

// Mock variables for unit testing
var linkAdd = netlink.LinkAdd
var linkDel = netlink.LinkDel
var linkSetUp = netlink.LinkSetUp
var linkByName = netlink.LinkByName
