package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	testerUtil "github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util"

	"github.com/stretchr/testify/assert"
)

// TestInfrastructureSetup blocks until the machines in the infrastructure have
// been deployed, or 10 minutes have passed. This way, the rest of the test
// suites will not have to account for booting the machines.
func TestInfrastructureSetup(t *testing.T) {
	clnt, _, err := testerUtil.GetDefaultDaemonClient()
	assert.NoError(t, err)

	bp, err := testerUtil.GetCurrentBlueprint(clnt)
	assert.NoError(t, err)

	machinesReady := func() bool {
		machines, err := clnt.QueryMachines()
		if err != nil {
			fmt.Println("Failed to list machines:", err)
			return false
		}

		for _, m := range machines {
			if m.Status != db.Connected {
				return false
			}
		}
		return len(machines) == len(bp.Machines)
	}
	err = util.BackoffWaitFor(machinesReady, 30*time.Second, 10*time.Minute)
	assert.NoError(t, err, "Not all machines in the infrastructure connected "+
		"back to the daemon. Any tests relying on all the machines will "+
		"likely fail")
}
