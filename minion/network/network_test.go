package network

import (
	"errors"
	"testing"

	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/minion/ipdef"
	"github.com/NetSys/quilt/minion/ovsdb"
	"github.com/NetSys/quilt/minion/ovsdb/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	conn := db.New()

	// Supervisor isn't initialized, nothing should happen.
	runMaster(conn)

	conn.Txn(db.AllTables...).Run(func(view db.Database) error {
		etcd := view.InsertEtcd()
		etcd.Leader = true
		view.Commit(etcd)

		minion := view.InsertMinion()
		minion.SupervisorInit = true
		minion.Self = true
		view.Commit(minion)

		label := view.InsertLabel()
		label.Label = "junk"
		view.Commit(label)

		c := view.InsertContainer()
		c.IP = "1.2.3.4"
		view.Commit(c)
		return nil
	})

	anErr := errors.New("err")

	client := new(mocks.Client)
	ovsdb.Open = func() (ovsdb.Client, error) { return nil, anErr }
	runMaster(conn)

	ovsdb.Open = func() (ovsdb.Client, error) {
		return client, nil
	}

	client.On("CreateLogicalSwitch", lSwitch).Return(nil)
	client.On("Disconnect").Return(nil)
	client.On("ListAddressSets").Return(nil, anErr)
	client.On("ListACLs").Return(nil, anErr)
	client.On("ListLogicalPorts").Return(nil, anErr).Once()

	runMaster(conn)
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "CreateLogicalSwitch", mock.Anything)

	client.On("ListLogicalPorts").Return([]ovsdb.LPort{{Name: "1.2.3.5"}}, nil)
	client.On("DeleteLogicalPort", lSwitch, ovsdb.LPort{
		Name: "1.2.3.5", Addresses: nil}).Return(anErr).Once()
	client.On("CreateLogicalPort", lSwitch, "1.2.3.4",
		"02:00:01:02:03:04", "1.2.3.4").Return(anErr).Once()
	runMaster(conn)
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "ListLogicalPorts")
	client.AssertCalled(t, "CreateLogicalSwitch", mock.Anything)
	client.AssertCalled(t, "DeleteLogicalPort", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateLogicalPort", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)

	client.On("ListLogicalPorts").Return([]ovsdb.LPort{{Name: "1.2.3.5"}}, nil)
	client.On("DeleteLogicalPort", lSwitch, ovsdb.LPort{
		Name: "1.2.3.5", Addresses: []string(nil)}).Return(nil)
	client.On("CreateLogicalPort", lSwitch, "1.2.3.4",
		"02:00:01:02:03:04", "1.2.3.4").Return(nil).Once()
	runMaster(conn)
	client.AssertCalled(t, "Disconnect")
	client.AssertCalled(t, "ListLogicalPorts")
	client.AssertCalled(t, "CreateLogicalSwitch", mock.Anything)
	client.AssertCalled(t, "DeleteLogicalPort", mock.Anything, mock.Anything)
	client.AssertCalled(t, "CreateLogicalPort", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything)
}

func TestGenerateOFPorts(t *testing.T) {
	t.Parallel()

	ifaces := map[string]int{
		"1":   101,
		"q_1": 201,
		"2":   102,
		"q_3": 203}
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
