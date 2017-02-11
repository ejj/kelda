package plugin

import (
	dnet "github.com/docker/go-plugins-helpers/network"
)

// CreateNetwork is a noop.
func (d driver) CreateNetwork(req *dnet.CreateNetworkRequest) error {
	return nil
}

// AllocateNetwork is a noop.
func (d driver) AllocateNetwork(req *dnet.AllocateNetworkRequest) (
	*dnet.AllocateNetworkResponse, error) {

	return &dnet.AllocateNetworkResponse{}, nil
}

// DeleteNetwork is a noop.
func (d driver) DeleteNetwork(req *dnet.DeleteNetworkRequest) error {
	return nil
}

// FreeNetwork is a noop.
func (d driver) FreeNetwork(req *dnet.FreeNetworkRequest) error {
	return nil
}

// DiscoverNew is a noop.
func (d driver) DiscoverNew(n *dnet.DiscoveryNotification) error {
	return nil
}

// DiscoverDelete is a noop.
func (d driver) DiscoverDelete(n *dnet.DiscoveryNotification) error {
	return nil
}

// Leave is a noop.
func (d driver) Leave(req *dnet.LeaveRequest) error {
	return nil
}

// ProgramExternalConnectivity is a noop.
func (d driver) ProgramExternalConnectivity(
	req *dnet.ProgramExternalConnectivityRequest) error {

	return nil
}

// RevokeExternalConnectivity is a noop.
func (d driver) RevokeExternalConnectivity(
	req *dnet.RevokeExternalConnectivityRequest) error {

	return nil
}
