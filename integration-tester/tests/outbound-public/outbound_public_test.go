package main

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util/str"
)

func TestOutboundPublic(t *testing.T) {
	clnt, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	connections, err := clnt.QueryConnections()
	if err != nil {
		t.Fatalf("couldn't query connections: %s", err)
	}

	test(t, containers, connections)
}

var testPort = 80
var testHost = fmt.Sprintf("google.com:%d", testPort)

func test(t *testing.T, containers []db.Container, connections []db.Connection) {
	connected := map[string]struct{}{}
	for _, conn := range connections {
		if str.SliceContains(conn.To, blueprint.PublicInternetLabel) &&
			inRange(testPort, conn.MinPort, conn.MaxPort) {
			for _, from := range conn.From {
				connected[from] = struct{}{}
			}
		}
	}

	for _, c := range containers {
		_, shouldPass := connected[c.Hostname]

		fmt.Printf("Fetching %s from container %s\n", testHost, c.BlueprintID)
		if shouldPass {
			fmt.Println(".. It should not fail")
		} else {
			fmt.Println(".. It should fail")
		}

		out, err := exec.Command("kelda", "ssh", c.BlueprintID,
			"wget", "-T", "2", "-O", "-", testHost).CombinedOutput()

		errored := err != nil
		if shouldPass && errored {
			t.Errorf("Fetch failed when it should have succeeded: %s", err)
			fmt.Println(string(out))
		} else if !shouldPass && !errored {
			t.Error("Fetch succeeded when it should have failed")
			fmt.Println(string(out))
		}
	}
}

func inRange(candidate, min, max int) bool {
	return min <= candidate && candidate <= max
}
