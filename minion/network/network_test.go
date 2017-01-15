package network

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/ovsdb"
	"github.com/stretchr/testify/assert"
)

type lportslice []ovsdb.LPort

func (lps lportslice) Len() int {
	return len(lps)
}

func (lps lportslice) Less(i, j int) bool {
	return lps[i].Name < lps[j].Name
}

func (lps lportslice) Swap(i, j int) {
	lps[i], lps[j] = lps[j], lps[i]
}

func TestRunMaster(t *testing.T) {
	client := ovsdb.NewFakeOvsdbClient()
	client.CreateLogicalSwitch(lSwitch)
	conn := db.New()
	ovsdb.Open = func() (ovsdb.Client, error) {
		return client, nil
	}

	expPorts := []ovsdb.LPort{}
	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.InsertEtcd()
		etcd.Leader = true
		view.Commit(etcd)

		minion := view.InsertMinion()
		minion.SupervisorInit = true
		minion.Self = true
		view.Commit(minion)

		for i := 3; i < 5; i++ {
			si := strconv.Itoa(i)
			c := view.InsertContainer()
			c.IP = fmt.Sprintf("0.0.0.%s", si)
			view.Commit(c)
			expPorts = append(expPorts, ovsdb.LPort{
				Bridge:    lSwitch,
				Name:      c.IP,
				Addresses: []string{ipdef.IPStrToMac(c.IP), c.IP},
			})
		}

		return nil
	})

	for i := 1; i < 6; i++ {
		si := strconv.Itoa(i)
		mac := fmt.Sprintf("00:00:00:00:00:0%s", si)
		if i < 3 {
			mac = labelMac
		}
		ip := fmt.Sprintf("0.0.0.%s", si)
		client.CreateLogicalPort(lSwitch, ip, mac, ip)
	}

	runMaster(conn)

	lports, err := client.ListLogicalPorts(lSwitch)
	if err != nil {
		t.Fatal("failed to fetch logical ports from mock client")
	}

	if len(lports) != len(expPorts) {
		t.Fatalf("wrong number of logical ports. Got %d, expected %d.",
			len(lports), len(expPorts))
	}

	sort.Sort(lportslice(lports))
	sort.Sort(lportslice(expPorts))
	for i, port := range expPorts {
		lport := lports[i]
		if lport.Bridge != port.Bridge || lport.Name != port.Name {
			t.Fatalf("Incorrect port %v, expected %v.", lport, port)
		}
	}
}

func checkAddressSet(t *testing.T, client ovsdb.Client,
	labels []db.Label, exp []ovsdb.AddressSet) {

	syncAddressSets(client, labels)
	actual, _ := client.ListAddressSets(lSwitch)

	ovsdbKey := func(intf interface{}) interface{} {
		addrSet := intf.(ovsdb.AddressSet)
		// OVSDB returns the addresses in a non-deterministic order, so we
		// sort them.
		sort.Strings(addrSet.Addresses)
		return addressSetKey{
			name:      addrSet.Name,
			addresses: strings.Join(addrSet.Addresses, " "),
		}
	}
	if _, lefts, rights := join.HashJoin(addressSlice(actual), addressSlice(exp),
		ovsdbKey, ovsdbKey); len(lefts) != 0 || len(rights) != 0 {
		t.Errorf("Wrong address sets: expected %v, got %v.", exp, actual)
	}
}

func TestAddressSetSync(t *testing.T) {
	t.Parallel()

	client := ovsdb.NewFakeOvsdbClient()
	client.CreateLogicalSwitch(lSwitch)

	redLabel := db.Label{
		Label:        "red",
		ContainerIPs: []string{"8.8.8.8"},
		IP:           "8.8.8.8",
	}
	blueLabel := db.Label{
		Label:        "blue",
		ContainerIPs: []string{"10.10.10.10", "11.11.11.11"},
		IP:           "9.9.9.9",
	}
	redAddressSet := ovsdb.AddressSet{
		Name:      "red",
		Addresses: []string{"8.8.8.8"},
	}
	blueAddressSet := ovsdb.AddressSet{
		Name:      "blue",
		Addresses: []string{"9.9.9.9", "10.10.10.10", "11.11.11.11"},
	}
	checkAddressSet(t, client,
		[]db.Label{redLabel},
		[]ovsdb.AddressSet{redAddressSet},
	)
	checkAddressSet(t, client,
		[]db.Label{redLabel, blueLabel},
		[]ovsdb.AddressSet{redAddressSet, blueAddressSet},
	)
	checkAddressSet(t, client,
		[]db.Label{blueLabel},
		[]ovsdb.AddressSet{blueAddressSet},
	)

	// Test hyphen conversion.
	dashLabel := db.Label{
		Label: "spark-ms",
		IP:    "9.9.9.9",
	}
	dashAddressSet := ovsdb.AddressSet{
		Name:      "SPARK_MS",
		Addresses: []string{"9.9.9.9"},
	}
	checkAddressSet(t, client,
		[]db.Label{dashLabel},
		[]ovsdb.AddressSet{dashAddressSet},
	)
}

func checkACLs(t *testing.T, client ovsdb.Client,
	connections []db.Connection, exp []ovsdb.ACL) {

	syncACLs(client, connections)

	actual, _ := client.ListACLs(lSwitch)

	ovsdbKey := func(ovsdbIntf interface{}) interface{} {
		return ovsdbIntf.(ovsdb.ACL).Core
	}
	if _, left, right := join.HashJoin(ovsdbACLSlice(actual), ovsdbACLSlice(exp),
		ovsdbKey, ovsdbKey); len(left) != 0 || len(right) != 0 {
		t.Errorf("Wrong ACLs: expected %v, got %v.", exp, actual)
	}
}

func TestACLSync(t *testing.T) {
	t.Parallel()

	client := ovsdb.NewFakeOvsdbClient()
	client.CreateLogicalSwitch(lSwitch)

	dropACLs := directedACLs(ovsdb.ACL{
		Core: ovsdb.ACLCore{
			Priority: 0,
			Match:    "ip",
			Action:   "drop",
		},
	})

	redBlueConnection := db.Connection{
		From:    "red",
		To:      "blue",
		MinPort: 80,
		MaxPort: 80,
	}
	redBlueACLs := directedACLs(ovsdb.ACL{
		Core: ovsdb.ACLCore{
			Priority: 1,
			Match: "(((ip4.src == $red && ip4.dst == $blue) && " +
				"(icmp || 80 <= udp.dst <= 80 || " +
				"80 <= tcp.dst <= 80)) || ((ip4.src == $blue && " +
				"ip4.dst == $red) && (icmp || 80 <= udp.src <= 80 || " +
				"80 <= tcp.src <= 80)))",
			Action: "allow",
		},
	})

	redYellowConnection := db.Connection{
		From:    "red",
		To:      "yellow",
		MinPort: 80,
		MaxPort: 81,
	}
	redYellowACLs := directedACLs(ovsdb.ACL{
		Core: ovsdb.ACLCore{
			Priority: 1,
			Match: "(((ip4.src == $red && ip4.dst == $yellow) && " +
				"(icmp || 80 <= udp.dst <= 81 || " +
				"80 <= tcp.dst <= 81)) || ((ip4.src == $yellow && " +
				"ip4.dst == $red) && (icmp || 80 <= udp.src <= 81 || " +
				"80 <= tcp.src <= 81)))",
			Action: "allow",
		},
	})

	checkACLs(t, client,
		[]db.Connection{redBlueConnection},
		append(dropACLs, redBlueACLs...),
	)
	checkACLs(t, client,
		[]db.Connection{redBlueConnection, redYellowConnection},
		append(dropACLs, append(redBlueACLs, redYellowACLs...)...),
	)
	checkACLs(t, client,
		[]db.Connection{redYellowConnection},
		append(dropACLs, redYellowACLs...),
	)

	// Test hyphen conversion.
	dashConnection := db.Connection{
		From:    "spark-ms",
		To:      "spark-wk",
		MinPort: 80,
		MaxPort: 80,
	}
	dashACLs := directedACLs(ovsdb.ACL{
		Core: ovsdb.ACLCore{
			Priority: 1,
			Match: "(((ip4.src == $SPARK_MS && ip4.dst == $SPARK_WK) && " +
				"(icmp || 80 <= udp.dst <= 80 || " +
				"80 <= tcp.dst <= 80)) || ((ip4.src == $SPARK_WK && " +
				"ip4.dst == $SPARK_MS) && " +
				"(icmp || 80 <= udp.src <= 80 || 80 <= tcp.src <= 80)))",
			Action: "allow",
		},
	})
	checkACLs(t, client,
		[]db.Connection{dashConnection},
		append(dropACLs, dashACLs...),
	)
}

func TestGenerateOFPorts(t *testing.T) {
	t.Parallel()

	var ptr = func(x int) *int {
		return &x
	}

	ifaces := []ovsdb.Interface{
		{Name: "1", OFPort: ptr(101)},
		{Name: "q_1", OFPort: ptr(201)},
		{Name: "2", OFPort: ptr(102)},
		{Name: "q_2", OFPort: ptr(-1)},
		{Name: "q_3", OFPort: ptr(203)},
	}

	containers := []db.Container{
		{EndpointID: "1", DockerID: "1", IP: "1.1.1.1"},
		{EndpointID: "2", DockerID: "2", IP: "2.2.2.2"},
		{EndpointID: "3", DockerID: "3", IP: "3.3.3.3"},
	}

	assert.Equal(t,
		[]ofPort{{VethPort: 101, PatchPort: 201,
			Mac: ipdef.IPStrToMac("1.1.1.1")}},
		generateOFPorts(ifaces, containers))
}
