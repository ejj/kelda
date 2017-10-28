package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

func TestDNS(t *testing.T) {
	clnt, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	dnsTester, err := newDNSTester(clnt)
	if err != nil {
		t.Fatalf("couldn't initialize dns tester: %s", err.Error())
	}

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err.Error())
	}

	// Run the test twice to see if failed tests persist.
	for i := 0; i < 2; i++ {
		testContainers(t, dnsTester, containers)
	}
}

func TestConnectivity(t *testing.T) {
	clnt, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	connTester, err := newConnectionTester(clnt)
	if err != nil {
		t.Fatalf("couldn't initialize connection tester: %s", err.Error())
	}

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err.Error())
	}

	// Run the test twice to see if failed tests persist.
	for i := 0; i < 2; i++ {
		testContainers(t, connTester, containers)
	}
}

type testerIntf interface {
	test(t *testing.T, c db.Container)
}

type commandTime struct {
	start, end time.Time
}

func (ct commandTime) String() string {
	// Just show the hour, minute, and second.
	timeFmt := "15:04:05"
	return ct.start.Format(timeFmt) + " - " + ct.end.Format(timeFmt)
}

// Gather test results for each container. For each minion machine, run one test
// at a time.
func testContainers(t *testing.T, tester testerIntf, containers []db.Container) {
	var wg sync.WaitGroup
	for _, c := range containers {
		wg.Add(1)
		go func(c db.Container) {
			tester.test(t, c)
			wg.Done()
		}(c)
	}
	wg.Wait()
}

// We have to limit our parallelization because each `kelda ssh` creates a new SSH login
// session. Doing this quickly in parallel breaks system-logind on the remote machine:
// https://github.com/systemd/systemd/issues/2925.  Furthermore, the concurrency limit
// cannot exceed the sshd MaxStartups setting, or else the SSH connections may be
// randomly rejected.
var sshRateLimit = time.Tick(100 * time.Millisecond)

func keldaSSH(id string, cmd ...string) (string, error) {
	<-sshRateLimit
	fmt.Printf("kelda ssh %s %s\n", id, strings.Join(cmd, " "))
	execCmd := exec.Command("kelda", append([]string{"ssh", id}, cmd...)...)
	stderrBytes := bytes.NewBuffer(nil)
	execCmd.Stderr = stderrBytes

	stdoutBytes, err := execCmd.Output()
	if err != nil {
		err = errors.New(stderrBytes.String())
	}

	return string(stdoutBytes), err
}
