package plugin

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/minion/ipdef"
	"github.com/kelda/kelda/minion/network/openflow"
	"github.com/kelda/kelda/minion/nl"

	dnet "github.com/docker/go-plugins-helpers/network"

	log "github.com/sirupsen/logrus"
)

const (
	// NetworkName is the name of the network driver plugin.
	NetworkName = "kelda"

	pluginDir   = "/run/docker/plugins"
	ifacePrefix = "eth"
)

var (
	networkSocket = NetworkName + ".sock"
	pluginSocket  = filepath.Join(pluginDir, networkSocket)
)

type driver struct{}

var c = counter.New("Network Plugin")

const mtu int = 1400

// Run runs the network driver and starts the server to listen for requests. It will
// block until the server socket has been created.
func Run() {
	h := dnet.NewHandler(driver{})

	go ofctlRun()
	go vsctlRun()

	go func() {
		err := h.ServeUnix(pluginSocket, 0600)
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
	c.Inc("Capabilities")
	return &dnet.CapabilitiesResponse{Scope: dnet.LocalScope}, nil
}

// CreateEndpoint acknowledges the request, but does not actually do anything.
func (d driver) CreateEndpoint(req *dnet.CreateEndpointRequest) (
	*dnet.CreateEndpointResponse, error) {
	c.Inc("Create Endpoint")

	addr, _, err := net.ParseCIDR(req.Interface.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid IP: %s", req.Interface.Address)
	}

	outer := ipdef.IFName(req.EndpointID)
	inner := ipdef.IFName("tmp_" + req.EndpointID)
	if err := nl.N.AddVeth(outer, inner, mtu); err != nil {
		return nil, fmt.Errorf("failed to create veth: %s", err)
	}

	outerLink, err := getOuterLink(req.EndpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to find link %s: %s", outer, err)
	}

	if err := nl.N.LinkSetUp(outerLink); err != nil {
		return nil, fmt.Errorf("failed to bring up link %s: %s", outer, err)
	}

	mac := ipdef.IPToMac(addr)
	peerBr, peerKelda := ipdef.PatchPorts(req.EndpointID)

	err = vsctl([][]string{
		{"add-port", ipdef.KeldaBridge, ipdef.IFName(req.EndpointID)},
		{"add-port", ipdef.KeldaBridge, peerKelda},
		{"set", "Interface", peerKelda, "type=patch", "options:peer=" + peerBr},
		{"add-port", ipdef.OvnBridge, peerBr},
		{"set", "Interface", peerBr, "type=patch", "options:peer=" + peerKelda,
			"external-ids:attached-mac=" + mac,
			"external-ids:iface-id=" + addr.String()}})
	if err != nil {
		return nil, fmt.Errorf("ovs-vsctl: %v", err)
	}

	err = ofctl(openflow.Container{
		Veth:  outer,
		Patch: peerKelda,
		Mac:   mac,
		IP:    addr.String()})
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
	c.Inc("Endpoint Info")
	if _, err := getOuterLink(req.EndpointID); err != nil {
		return nil, err
	}
	return &dnet.InfoResponse{}, nil
}

// DeleteEndpoint cleans up state associated with a docker endpoint.
func (d driver) DeleteEndpoint(req *dnet.DeleteEndpointRequest) error {
	c.Inc("Delete Endpoint")
	peerBr, peerKelda := ipdef.PatchPorts(req.EndpointID)
	err := vsctl([][]string{
		{"del-port", ipdef.KeldaBridge, ipdef.IFName(req.EndpointID)},
		{"del-port", ipdef.KeldaBridge, peerKelda},
		{"del-port", ipdef.OvnBridge, peerBr}})
	if err != nil {
		return fmt.Errorf("ovs-vsctl: %v", err)
	}

	outer, err := getOuterLink(req.EndpointID)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %s", req.EndpointID, err)
	}

	if err := nl.N.LinkDel(outer); err != nil {
		return fmt.Errorf("failed to delete link %s: %s", req.EndpointID, err)
	}

	return nil
}

// Join method is invoked when a Sandbox is attached to an endpoint.
func (d driver) Join(req *dnet.JoinRequest) (*dnet.JoinResponse, error) {
	c.Inc("Join")
	inner := ipdef.IFName("tmp_" + req.EndpointID)
	resp := &dnet.JoinResponse{}
	resp.Gateway = ipdef.GatewayIP.String()
	resp.InterfaceName = dnet.InterfaceName{SrcName: inner, DstPrefix: ifacePrefix}
	return resp, nil
}

func getOuterLink(eid string) (nl.Link, error) {
	return nl.N.LinkByName(ipdef.IFName(eid))
}
