package main

import (
	"testing"

	"github.com/kelda/kelda/integration-tester/util"
)

func TestLobsters(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err.Error())
	}

	connections, err := clnt.QueryConnections()
	if err != nil {
		t.Fatalf("couldn't query connections: %s", err.Error())
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err.Error())
	}

	util.CheckPublicConnections(t, machines, containers, connections)
}
