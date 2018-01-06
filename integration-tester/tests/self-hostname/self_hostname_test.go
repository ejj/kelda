package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/kelda/kelda/integration-tester/util"

	"github.com/stretchr/testify/assert"
)

// TestSelfHostname checks that the hostname each container believes itself to
// have (as queried by the `hostname` command) can actually be used to route to
// the container. It expects to be run against a deployment of web containers
// (with the hostname prefix "web") that return their hostname when queried,
// and a fetcher container that is connected to all the web containers.
func TestSelfHostname(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	assert.NoError(t, err)
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	assert.NoError(t, err)

	var reportedHostnames []string
	for _, c := range containers {
		if !strings.HasPrefix(c.Hostname, "web") {
			continue
		}

		reportedHostname, err := containerExec(c.BlueprintID, "hostname")
		assert.NoError(t, err)
		reportedHostname = strings.TrimRight(reportedHostname, "\n")
		fmt.Printf("Container %+v reported its hostname as %s\n",
			c, reportedHostname)
		reportedHostnames = append(reportedHostnames, reportedHostname)
	}

	// Assert that the hostname each container believes itself to have
	// actually routes to the container.
	for _, hostname := range reportedHostnames {
		actualHostname, err := containerExec("fetcher", "wget", "-O", "-",
			hostname)
		assert.NoError(t, err)

		actualHostname = strings.TrimRight(actualHostname, "\n")
		fmt.Printf("Container at hostname %s is actually container %s\n",
			hostname, actualHostname)
		assert.Equal(t, hostname, actualHostname, "the container's self-"+
			"reported hostname did not route to the container")
	}
}

func containerExec(containerID string, commands ...string) (string, error) {
	cmd := exec.Command("kelda", append([]string{"ssh", containerID},
		commands...)...)
	stderrBytes := bytes.NewBuffer(nil)
	cmd.Stderr = stderrBytes
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s (stderr: %s)", err, stderrBytes.String())
	}

	return string(output), nil
}
