package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	testerUtil "github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util"

	"github.com/stretchr/testify/assert"
)

// TestSwitchNamespace tests that if a user switches namespaces then runs the
// same blueprint, containers are not restarted.
func TestSwitchNamespace(t *testing.T) {
	clnt, _, err := testerUtil.GetDefaultDaemonClient()
	assert.NoError(t, err)

	initialBlueprint, err := testerUtil.GetCurrentBlueprint(clnt)
	assert.NoError(t, err)

	initialContainers, err := clnt.QueryContainers()
	assert.NoError(t, err)

	machines, err := clnt.QueryMachines()
	assert.NoError(t, err)

	privToPubIP := map[string]string{}
	for _, dbm := range machines {
		privToPubIP[dbm.PrivateIP] = dbm.PublicIP
	}

	initialContainerStartTimes, err := getContainerStartTimes(
		privToPubIP, initialContainers)
	assert.NoError(t, err)

	fmt.Println("Simulating switching namespaces by running `kelda stop`")
	cmd := exec.Command("kelda", "stop", "-f", "random-namespace")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	assert.NoError(t, cmd.Run())

	// Wait until the daemon has switched namespaces by waiting for it to pick
	// up that there are no machines in the new namespace.
	switchedNamespace := func() bool {
		machines, err := clnt.QueryMachines()
		if err != nil {
			fmt.Println("Failed to list machines:", err)
			return false
		}
		return len(machines) == 0
	}
	err = util.BackoffWaitFor(switchedNamespace, 30*time.Second, 3*time.Minute)
	assert.NoError(t, err)

	// Sleep an additional 30 seconds in case the daemon does something odd in
	// the new namespace.
	time.Sleep(30 * time.Second)

	fmt.Println("Switching back to the original blueprint")
	assert.NoError(t, clnt.Deploy(initialBlueprint.String()))

	// Wait until the daemon has redeployed the initial blueprint (i.e. wait
	// until the specified containers are tracked by the cluster).
	containersUp := func() bool {
		containers, err := clnt.QueryContainers()
		if err != nil {
			fmt.Println("Failed to list containers:", err)
			return false
		}

		return len(containers) == len(initialBlueprint.Containers)
	}
	err = util.BackoffWaitFor(containersUp, 30*time.Second, 3*time.Minute)
	assert.NoError(t, err)

	// Sleep an additional minute to give the cluster time to evaluate the
	// blueprint and possibly make any changes.
	time.Sleep(1 * time.Minute)

	// Ensure that the running containers are exactly the same as before we
	// switched namespaces.
	currentContainers, err := clnt.QueryContainers()
	assert.NoError(t, err)

	currentContainerStartTimes, err := getContainerStartTimes(
		privToPubIP, currentContainers)
	assert.NoError(t, err)

	fmt.Printf("Initial containers: %+v\n", initialContainerStartTimes)
	fmt.Printf("Current containers: %+v\n", currentContainerStartTimes)
	assert.Equal(t, initialContainerStartTimes, currentContainerStartTimes)
}

func getContainerStartTimes(privToPubIP map[string]string, dbcs []db.Container) (
	map[string]time.Time, error) {

	hostnameToStartTime := map[string]time.Time{}
	for _, dbc := range dbcs {
		pubIP, ok := privToPubIP[dbc.Minion]
		if !ok {
			return nil, fmt.Errorf("no public IP for %s", dbc.Minion)
		}

		addr := "http://" + pubIP
		resp, err := http.Get(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to GET address %s: %s", addr, err)
		}

		respBodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read GET response from %s: %s",
				addr, err)
		}
		respBody := strings.TrimRight(string(respBodyBytes), "\n")

		startTime, err := time.Parse(time.UnixDate, respBody)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time %s: %s",
				respBody, err)
		}
		hostnameToStartTime[dbc.Hostname] = startTime
	}
	return hostnameToStartTime, nil
}
