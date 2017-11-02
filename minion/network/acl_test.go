package network

import (
	"errors"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/ovsdb"
	"github.com/kelda/kelda/minion/ovsdb/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestResolveConenctions(t *testing.T) {
	t.Parallel()

	connections, addressSets := resolveConnections([]db.Connection{{
		From: []string{"public"},
		To:   []string{"a"},
	}, {
		From: []string{"a"},
		To:   []string{"badHostname"},
	}, {
		From:    []string{"a", "b"},
		To:      []string{"c"},
		MinPort: 1,
		MaxPort: 2,
	}, {
		From:    []string{"a", "b", "c"},
		To:      []string{"a", "b"},
		MinPort: 3,
		MaxPort: 4,
	}, {
		From:    []string{"a"},
		To:      []string{"a", "c"},
		MinPort: 5,
		MaxPort: 6,
	}, {
		From:    []string{"a", "c"},
		To:      []string{"a", "b", "c"},
		MinPort: 7,
		MaxPort: 8,
	}}, map[string]string{
		"a": "1.1.1.1",
		"b": "2.2.2.2",
		"c": "3.3.3.3",
	})

	assert.Equal(t, []connection{{
		from: "$sha886f76b8da8aa4cb490c3c9e7c8e6" +
			"df5add0c795ed90b460f0b7765bba4d2bc3",
		to:      "3.3.3.3",
		minPort: 1,
		maxPort: 2,
	}, {
		from: "$shad318437b8c796a3a7470bbc8f8457" +
			"41f4e9bc7478b32191a1f8e2bb215a2bef8",
		to: "$sha886f76b8da8aa4cb490c3c9e7c8e6" +
			"df5add0c795ed90b460f0b7765bba4d2bc3",
		minPort: 3,
		maxPort: 4,
	}, {
		from: "1.1.1.1",
		to: "$sha7d3647dab65e8fe823c7b3ffdd738" +
			"59e88450368c683c0cbcdb1c8d7b348a9d9",
		minPort: 5,
		maxPort: 6,
	}, {
		from: "$sha7d3647dab65e8fe823c7b3ffdd738" +
			"59e88450368c683c0cbcdb1c8d7b348a9d9",
		to: "$shad318437b8c796a3a7470bbc8f8457" +
			"41f4e9bc7478b32191a1f8e2bb215a2bef8",
		minPort: 7,
		maxPort: 8,
	}}, connections)

	exp := map[string][]string{
		"sha886f76b8da8aa4cb490c3c9e7c8e6df5add0c795ed90b460f0b7" +
			"765bba4d2bc3": {"1.1.1.1", "2.2.2.2"},
		"shad318437b8c796a3a7470bbc8f845741f4e9bc7478b32191a1f8e" +
			"2bb215a2bef8": {"1.1.1.1", "2.2.2.2", "3.3.3.3"},
		"sha7d3647dab65e8fe823c7b3ffdd73859e88450368c683c0cbcdb1" +
			"c8d7b348a9d9": {"1.1.1.1", "3.3.3.3"},
	}

	actual := map[string][]string{}
	for _, as := range addressSets {
		if _, ok := actual[as.Name]; ok {
			t.Errorf("Duplicate address set %s", as)
		}
		actual[as.Name] = as.Addresses
	}

	assert.Equal(t, exp, actual)

}

func TestSyncAddressSets(t *testing.T) {
	t.Parallel()

	client := new(mocks.Client)
	client.On("ListAddressSets").Return(nil, assert.AnError).Once()
	syncAddressSets(client, nil)
	client.AssertCalled(t, "ListAddressSets")

	client.On("ListAddressSets").Return([]ovsdb.AddressSet{{Name: "old"}}, nil)

	client.On("DeleteAddressSets", []ovsdb.AddressSet{{Name: "old"}}).Return(
		assert.AnError).Once()
	client.On("CreateAddressSets", []ovsdb.AddressSet{{Name: "new"}}).Return(
		assert.AnError).Once()
	syncAddressSets(client, []ovsdb.AddressSet{{Name: "new"}})
	client.AssertExpectations(t)

	client.On("DeleteAddressSets", []ovsdb.AddressSet{{Name: "old"}}).Return(nil)
	client.On("CreateAddressSets", []ovsdb.AddressSet{{Name: "new"}}).Return(nil)
	syncAddressSets(client, []ovsdb.AddressSet{{Name: "new"}})
	client.AssertExpectations(t)
}

func TestSyncACLs(t *testing.T) {
	t.Parallel()
	client := new(mocks.Client)

	anErr := errors.New("err")
	client.On("ListACLs").Return(nil, anErr).Once()
	syncACLs(client, nil)
	client.AssertCalled(t, "ListACLs")

	conns := []connection{
		{
			from:    "8.8.8.8",
			to:      "9.9.9.9",
			minPort: 80,
			maxPort: 80,
		}, {
			from:    "8.8.8.8",
			to:      "9.9.9.9",
			minPort: 8080,
			maxPort: 8080,
		},
	}
	core := ovsdb.ACLCore{Match: "a"}
	client.On("ListACLs").Return([]ovsdb.ACL{{Core: core}}, nil)

	client.On("CreateACLs", "kelda", mock.Anything).Return(nil).Once()
	client.On("DeleteACLs", mock.Anything, mock.Anything).Return(anErr).Once()
	syncACLs(client, conns)

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
		Action:    "allow-related",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     and(from("8.8.8.8"), to("9.9.9.9"), "icmp"),
		Action:    "allow-related",
	}, {
		Priority:  1,
		Direction: "from-lport",
		Match:     getMatchString(connection{"8.8.8.8", "9.9.9.9", 80, 80}),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     getMatchString(connection{"8.8.8.8", "9.9.9.9", 80, 80}),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "from-lport",
		Match:     getMatchString(connection{"8.8.8.8", "9.9.9.9", 8080, 8080}),
		Action:    "allow",
	}, {
		Priority:  1,
		Direction: "to-lport",
		Match:     getMatchString(connection{"8.8.8.8", "9.9.9.9", 8080, 8080}),
		Action:    "allow",
	}}

	assert.Equal(t, len(actualACLs), len(expACLs))
	assert.Subset(t, actualACLs, expACLs)
	assert.Subset(t, expACLs, actualACLs)

	client.AssertCalled(t, "ListACLs")

	client.On("CreateACLs", mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything).Return(anErr)
	client.On("DeleteACLs", mock.Anything, mock.Anything).Return(anErr).Once()
	syncACLs(client, conns)
	client.AssertCalled(t, "ListACLs")
}
