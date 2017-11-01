package network

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/minion/ovsdb/mocks"
	"github.com/stretchr/testify/mock"
)

func TestUpdateACLs(t *testing.T) {
	t.Parallel()
	client := new(mocks.Client)

	anErr := errors.New("err")
	client.On("ListACLs").Return(nil, anErr).Once()
	updateACLs(client, nil, nil)
	client.AssertCalled(t, "ListACLs")

	conns := []db.Connection{
		{
			From:    blueprint.PublicInternetLabel,
			To:      "ignoreme",
			MinPort: 80,
			MaxPort: 80,
		}, {
			From:    "b",
			To:      "c",
			MinPort: 80,
			MaxPort: 80,
		}, {
			From:    "b",
			To:      "c",
			MinPort: 8080,
			MaxPort: 8080,
		},
	}
	hostnameToIP := map[string]string{"b": "8.8.8.8", "c": "9.9.9.9"}
	core := ovsdb.ACLCore{Match: "a"}
	client.On("ListACLs").Return([]ovsdb.ACL{{Core: core}}, nil)

	client.On("CreateACL", lSwitch, "to-lport", 0, "ip", "drop").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 0, "ip", "drop").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 1, getMatchString(
		"8.8.8.8", "9.9.9.9", 80, 80), "allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "to-lport", 1, getMatchString(
		"8.8.8.8", "9.9.9.9", 80, 80), "allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 1, getMatchString(
		"8.8.8.8", "9.9.9.9", 8080, 8080), "allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "to-lport", 1, getMatchString(
		"8.8.8.8", "9.9.9.9", 8080, 8080), "allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "from-lport", 1,
		and(from("8.8.8.8"), to("9.9.9.9"), "icmp"), "allow").Return(nil).Once()
	client.On("CreateACL", lSwitch, "to-lport", 1,
		and(from("8.8.8.8"), to("9.9.9.9"), "icmp"), "allow").Return(nil).Once()
	client.On("DeleteACL", mock.Anything, mock.Anything).Return(anErr).Once()
	updateACLs(client, conns, hostnameToIP)
	client.AssertCalled(t, "ListACLs")
	client.AssertCalled(t, "DeleteACL", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)

	client.On("CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(anErr)
	client.On("DeleteACL", mock.Anything, mock.Anything).Return(anErr).Once()
	updateACLs(client, conns, hostnameToIP)
	client.AssertCalled(t, "ListACLs")
	client.AssertCalled(t, "DeleteACL", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateACL", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)
}
