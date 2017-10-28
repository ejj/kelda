package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

// The maximum number of SSH Sessions spread across all minions.
const MaxSSHSessions = 512

// Map from minion private IP address to a channel storing SSH clients to that minion.
// The channel is intended to act as a sempahore.  When needed a client can be popped off
// the front, and then pushed on the back when finished.
var clients = map[string]chan ssh.Client{}

func TestNetwork(t *testing.T) {
	clnt, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err.Error())
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("failed to query machines: %s", err)
	}

	// Map writes aren't thread safe, so we create the channels before go routines
	// have a chance to launch.
	for _, m := range machines {
		clients[m.PrivateIP] = make(chan ssh.Client, MaxSSHSessions)
	}

	// We have to limit parallelization setting up SSH sessions.  Doing so too
	// quickly in parallel breaks system-logind on the remote machine:
	// https://github.com/systemd/systemd/issues/2925.  Furthermore, the concurrency
	// limit cannot exceed the sshd MaxStartups setting, or else the SSH connections
	// may be randomly rejected.
	//
	// Also note, we intentionally don't wait for this go routine to finish.  As new
	// SSH connections are created, the tests can gradually take advantage of them.
	go func() {
		for i := 0; i < MaxSSHSessions; i++ {
			m := machines[i%len(machines)]
			client, err := ssh.New(m.PublicIP, "")
			if err != nil {
				t.Fatalf("failed to ssh to %s: %s", m.PublicIP, err)
			}
			clients[m.PrivateIP] <- client
		}
	}()

	t.Run("Ping", func(t *testing.T) {
		connTester, err := newConnectionTester(clnt)
		if err != nil {
			t.Fatalf("couldn't initialize connection tester: %s",
				err.Error())
		}

		testContainers(t, connTester, containers)
	})

	t.Run("DNS", func(t *testing.T) {
		dnsTester, err := newDNSTester(clnt)
		if err != nil {
			t.Fatalf("couldn't initialize dns tester: %s", err.Error())
		}

		testContainers(t, dnsTester, containers)
	})
}

type testerIntf interface {
	test(t *testing.T, c db.Container)
}

// Gather test results for each container. For each minion machine, run one test
// at a time.
func testContainers(t *testing.T, tester testerIntf, containers []db.Container) {
	var wg sync.WaitGroup
	wg.Add(len(containers))
	for _, c := range containers {
		go func(c db.Container) {
			tester.test(t, c)
			wg.Done()
		}(c)
	}
	wg.Wait()
}

func keldaSSH(dbc db.Container, cmd ...string) (string, error) {
	if dbc.Minion == "" || dbc.DockerID == "" {
		return "", errors.New("container not yet booted")
	}

	ssh := <-clients[dbc.Minion]
	defer func() { clients[dbc.Minion] <- ssh }()

	fmt.Println(dbc.BlueprintID, strings.Join(cmd, " "))
	ret, err := ssh.CombinedOutput(fmt.Sprintf("docker exec %s %s", dbc.DockerID,
		strings.Join(cmd, " ")))
	return string(ret), err
}
