package plugin

import (
	"testing"

	dnet "github.com/docker/go-plugins-helpers/network"
	"github.com/stretchr/testify/assert"
)

func TestNoop(t *testing.T) {
	setup()
	d := driver{}

	err := d.CreateNetwork(&dnet.CreateNetworkRequest{})
	assert.NoError(t, err)

	err = d.FreeNetwork(&dnet.FreeNetworkRequest{})
	assert.NoError(t, err)

	err = d.DiscoverNew(&dnet.DiscoveryNotification{})
	assert.NoError(t, err)

	err = d.DiscoverDelete(&dnet.DiscoveryNotification{})
	assert.NoError(t, err)

	err = d.ProgramExternalConnectivity(&dnet.ProgramExternalConnectivityRequest{})
	assert.NoError(t, err)

	err = d.RevokeExternalConnectivity(&dnet.RevokeExternalConnectivityRequest{})
	assert.NoError(t, err)

	err = d.Leave(&dnet.LeaveRequest{})
	assert.NoError(t, err)

	resp, err := d.AllocateNetwork(&dnet.AllocateNetworkRequest{})
	assert.NoError(t, err)

	if resp.Options != nil && len(resp.Options) > 0 {
		t.Fatalf("AllocateNetwork responded with non-empty response: %v", *resp)
	}

	err = d.DeleteNetwork(&dnet.DeleteNetworkRequest{})
	assert.NoError(t, err)
}
