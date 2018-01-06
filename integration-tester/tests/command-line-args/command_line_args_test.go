package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kelda/kelda/blueprint"
	"github.com/kelda/kelda/integration-tester/util"
	"github.com/stretchr/testify/assert"
)

// TestCmdLineArgs tests that the right number of containers are booted, and
// that they have the right hostname prefix. Both are based on the command
// line arguments passed to the blueprint with kelda run.
func TestCmdLineArgs(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatal("couldn't get api client")
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatal("couldn't query containers")
	}
	assert.Empty(t, containers)

	// We use the absolute path to avoid any fragility around relative paths,
	// when the test is run from different directories.
	testDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		t.Fatalf("failed to get absolute path: %s", err)
	}

	// The blueprint is in a subdirectory to avoid the test suite trying
	// to run it as part of the test.
	blueprintPath := filepath.Join(testDir, "dep", "withArgs.js")

	expCount := 5
	expHostname := "taylor-swift"

	cmd := exec.Command("kelda", "run", "-f", blueprintPath,
		strconv.Itoa(expCount), expHostname)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run blueprint: %s", err)
	}

	blueprintArgs := []string{strconv.Itoa(expCount), expHostname}
	bp, err := blueprint.FromFileWithArgs(blueprintPath, blueprintArgs)
	if err != nil {
		t.Fatalf("failed to compile blueprint: %s", err)
	}

	err = util.WaitForContainers(bp)
	if err != nil {
		t.Fatal("failed to start new containers")
	}

	containers, err = clnt.QueryContainers()
	if err != nil {
		t.Fatal("couldn't query containers the second time")
	}

	assert.Len(t, containers, expCount)
	for _, c := range containers {
		assert.Contains(t, c.Hostname, expHostname)
	}
}
