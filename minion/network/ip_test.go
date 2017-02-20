package network

import (
	"fmt"
	"net"
	"sort"
	"testing"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion/ipdef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocateContainerIPs(t *testing.T) {
	conn := db.New()

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		dbc := view.InsertContainer()
		dbc.IP = "10.0.0.2"
		dbc.StitchID = "1"
		view.Commit(dbc)

		dbc = view.InsertContainer()
		dbc.StitchID = "2"
		view.Commit(dbc)

		allocateContainerIPs(view)
		return nil
	})

	dbcs := conn.SelectFromContainer(nil)
	assert.Len(t, dbcs, 2)

	sort.Sort(db.ContainerSlice(dbcs))

	dbc := dbcs[0]
	dbc.ID = 0
	assert.Equal(t, db.Container{IP: "10.0.0.2", StitchID: "1"}, dbc)

	dbc = dbcs[1]
	assert.Equal(t, "2", dbc.StitchID)
	assert.True(t, ipdef.QuiltSubnet.Contains(net.ParseIP(dbc.IP)))
}

func TestUpdateLabelIPs(t *testing.T) {
	conn := db.New()

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		dbc := view.InsertContainer()
		dbc.Labels = []string{"red", "blue"}
		dbc.StitchID = "1"
		dbc.IP = "1.1.1.1"
		view.Commit(dbc)

		dbc = view.InsertContainer()
		dbc.Labels = []string{"red"}
		dbc.StitchID = "2"
		dbc.IP = "2.2.2.2"
		view.Commit(dbc)

		label := view.InsertLabel()
		label.Label = "yellow"
		view.Commit(label)

		return nil
	})

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		assert.NoError(t, updateLabelIPs(view))
		return nil
	})

	var labels []db.Label
	for _, label := range conn.SelectFromLabel(nil) {
		label.ID = 0
		labels = append(labels, label)
	}
	sort.Sort(db.LabelSlice(labels))

	assert.Equal(t, []db.Label{
		{
			Label:        "blue",
			IP:           "1.1.1.1",
			ContainerIPs: []string{"1.1.1.1"},
		}, {
			Label:        "red",
			IP:           "1.1.1.1",
			ContainerIPs: []string{"1.1.1.1", "2.2.2.2"},
		},
	}, labels)
}

func TestAllocate(t *testing.T) {
	subnet := net.IPNet{
		IP:   net.IPv4(0xab, 0xcd, 0xe0, 0x00),
		Mask: net.CIDRMask(20, 32),
	}
	conflicts := map[string]struct{}{}
	ipSet := map[string]struct{}{}

	// Only 4k IPs, in 0xfffff000. Guaranteed a collision
	for i := 0; i < 5000; i++ {
		ip, err := allocateIP(ipSet, subnet)
		if err != nil {
			continue
		}

		if _, ok := conflicts[ip]; ok {
			t.Fatalf("IP Double allocation: 0x%x", ip)
		}

		require.True(t, subnet.Contains(net.ParseIP(ip)),
			fmt.Sprintf("\"%s\" is not in %s", ip, subnet))
		conflicts[ip] = struct{}{}
	}

	assert.Equal(t, len(conflicts), len(ipSet))
	if len(conflicts) < 2500 || len(conflicts) > 4096 {
		// If the code's working, this is possible but *extremely* unlikely.
		// Probably a bug.
		t.Errorf("Too few conflicts: %d", len(conflicts))
	}
}
