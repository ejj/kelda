package network

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/minion/ovsdb/mocks"
	"github.com/stretchr/testify/assert"
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

	client.On("CreateACLs", "kelda", mock.Anything).Return(nil).Once()
	client.On("DeleteACLs", mock.Anything, mock.Anything).Return(anErr).Once()
	updateACLs(client, conns, hostnameToIP)

	// The order to CreateACLs is not deterministic, so we have to check that it
	// was called properly after the fact.
	var actualACLs []ovsdb.ACLCore
	for _, call := range client.Calls {
		if call.Method == "CreateACLs" {
			actualACLs = call.Arguments.Get(1).([]ovsdb.ACLCore)
			break
		}
	}

	expACLs := []ovsdb.ACLCore{{
		Priority:  0,
		Direction: "from-lport",
		Match:     "ip",
		Action:    "drop",
	}, {
		Priority:  0,
		Direction: "to-lport",
		Match:     "ip",
		Action:    "drop",
	}, {
		Priority:  1,
		Direction: "from-lport",
		Match:     and(from("8.8.8.8"), to("9.9.9.9"), "icmp"),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     and(from("8.8.8.8"), to("9.9.9.9"), "icmp"),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "from-lport",
		Match:     getMatchString("8.8.8.8", "9.9.9.9", 80, 80),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     getMatchString("8.8.8.8", "9.9.9.9", 80, 80),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "from-lport",
		Match:     getMatchString("8.8.8.8", "9.9.9.9", 8080, 8080),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     getMatchString("8.8.8.8", "9.9.9.9", 8080, 8080),
		Action:    "allow",
	}}

	assert.Equal(t, len(actualACLs), len(expACLs))
	assert.Subset(t, actualACLs, expACLs)
	assert.Subset(t, expACLs, actualACLs)

	client.AssertCalled(t, "ListACLs")

	client.On("CreateACLs", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(anErr)
	client.On("DeleteACLs", mock.Anything, mock.Anything).Return(anErr).Once()
	updateACLs(client, conns, hostnameToIP)
	client.AssertCalled(t, "ListACLs")
}
