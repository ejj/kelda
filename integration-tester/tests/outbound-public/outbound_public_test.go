package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util/str"
)

func TestOutboundPublic(t *testing.T) {
	clnt, creds, err := util.GetDefaultDaemonClient()
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

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	sshUtil, err := util.NewSSHUtil(machines, creds)
	if err != nil {
		t.Fatalf("failed to create SSH util client: %s", err)
	}

	test(t, sshUtil, containers, connections)
}

var testPort = 80

func test(t *testing.T, sshUtil util.SSHUtil, containers []db.Container,
	connections []db.Connection) {
	connected := map[string]struct{}{}
	for _, conn := range connections {
		if str.SliceContains(conn.To, blueprint.PublicInternetLabel) &&
			inRange(testPort, conn.MinPort, conn.MaxPort) {
			for _, from := range conn.From {
				connected[from] = struct{}{}
			}
		}
	}

	var wg sync.WaitGroup
	for _, c := range containers {
		for _, host := range []string{"google.com", "wikipedia.org"} {
			wg.Add(1)
			go func(c db.Container, host string) {
				defer wg.Done()

				out, err := sshUtil.SSH(c, "curl", "--connect-timeout",
					"10", "--verbose", host)
				errored := err != nil

				var errMsg string
				_, shouldPass := connected[c.Hostname]
				if shouldPass && errored {
					errMsg = "Fetch failed unexpectedly"
				} else if !shouldPass && !errored {
					errMsg = "Fetch succeeded unexpectedly"
				}

				if errMsg != "" {
					failMsg := fmt.Sprintf("%s -> %s: %s\n"+
						"Error: %s\n"+
						"Output: %s\n",
						c.Hostname, host, errMsg, err, out)
					fmt.Println(failMsg)
					t.Error(failMsg)
				}
			}(c, host)
		}
	}
	wg.Wait()
}

func inRange(candidate, min, max int) bool {
	return min <= candidate && candidate <= max
}
