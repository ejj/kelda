package network

import (
	"errors"
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/ovsdb"
	"github.com/quilt/quilt/minion/ovsdb/mocks"
	"github.com/quilt/quilt/stitch"
	"github.com/stretchr/testify/mock"
)

func TestSyncAddressSets(t *testing.T) {
	t.Parallel()
	client := new(mocks.Client)

	anErr := errors.New("err")
	client.On("ListAddressSets").Return(nil, anErr).Once()
	syncAddressSets(client, nil)
	client.AssertCalled(t, "ListAddressSets")

	labels := []db.Label{{
		Label: stitch.PublicInternetLabel,
	}, {
		Label: "b-b",
		IP:    "1.2.3.5"}}
	client.On("ListAddressSets").Return(
		[]ovsdb.AddressSet{{Name: "a", Addresses: []string{"1.2.3.4"}}}, nil)

	client.On("DeleteAddressSet", "a").Return(anErr).Once()
	client.On("CreateAddressSet", "B_B", []string{"1.2.3.5"}).Return(anErr).Once()
	syncAddressSets(client, labels)
	client.AssertCalled(t, "ListAddressSets")
	client.AssertCalled(t, "DeleteAddressSet", mock.Anything)
	client.AssertCalled(t, "CreateAddressSet", mock.Anything, mock.Anything)

	client.On("DeleteAddressSet", "a").Return(nil).Once()
	client.On("CreateAddressSet", "B_B", []string{"1.2.3.5"}).Return(nil).Once()
	syncAddressSets(client, labels)
	client.AssertCalled(t, "ListAddressSets")
	client.AssertCalled(t, "DeleteAddressSet", mock.Anything)
	client.AssertCalled(t, "CreateAddressSet", mock.Anything, mock.Anything)
}

func TestSyncACLs(t *testing.T) {
	t.Parallel()
	client := new(mocks.Client)

	anErr := errors.New("err")
	client.On("ListACLs").Return(nil, anErr).Once()
	syncACLs(client, nil)
	client.AssertCalled(t, "ListACLs")

	conns := []db.Connection{{From: stitch.PublicInternetLabel}, {From: "b"}}
	core := ovsdb.ACLCore{Match: "a"}
	client.On("ListACLs").Return([]ovsdb.ACL{{Core: core}}, nil)

	client.On("CreateACL", lSwitch, "to-lport", 0, "ip", "drop").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 0, "ip", "drop").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 1, matchString(conns[1]),
		"allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "to-lport", 1, matchString(conns[1]),
		"allow").Return(nil).Once()
	client.On("DeleteACL", mock.Anything, mock.Anything).Return(anErr).Once()
	syncACLs(client, conns)
	client.AssertCalled(t, "ListACLs")
	client.AssertCalled(t, "DeleteACL", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)

	client.On("CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(anErr)
	client.On("DeleteACL", mock.Anything, mock.Anything).Return(anErr).Once()
	syncACLs(client, conns)
	client.AssertCalled(t, "ListACLs")
	client.AssertCalled(t, "DeleteACL", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)
}
