package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kelda/kelda/integration-tester/util"
)

func TestMinionLogs(t *testing.T) {
	if err := printKeldaPs(); err != nil {
		t.Errorf("failed to print kelda ps: %s", err.Error())
	}

	c, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer c.Close()

	machines, err := c.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query the machines: %s", err.Error())
	}

	for _, machine := range machines {
		fmt.Println(machine)
		logOutput, err := exec.Command("kelda", "ssh", machine.CloudID,
			"sudo", "journalctl", "-o", "cat", "-u", "minion").
			CombinedOutput()
		if err != nil {
			t.Errorf("unable to get minion logs: %s", err.Error())
			continue
		}
		logOutputStr := string(logOutput)
		fmt.Println(logOutputStr)
		checkLogs(t, logOutputStr)
	}
}

func TestDaemonLogs(t *testing.T) {
	daemonLoggerPath := filepath.Join(os.Getenv("WORKSPACE"), "daemonOutput.log")
	daemonLogOutput, err := ioutil.ReadFile(daemonLoggerPath)
	if err != nil {
		t.Fatalf("couldn't read daemon output: %s", err.Error())
	}

	checkLogs(t, string(daemonLogOutput))
}

var ignoreRegexes = []*regexp.Regexp{
	// DigitalOcean API calls sometimes randomly fail with a 500 error:
	regexp.MustCompile("(?:ERROR|WARNING) \\[.*? 500 Server was " +
		"unable to give you a response\\.$"),

	// This is a seemingly harmless DigitalOcean API error, cause unknown.
	regexp.MustCompile("(?:ERROR|WARNING) \\[.*? invalid character '<' " +
		"looking for beginning of value$"),

	// Errors pulling an image are completely out of our control.
	regexp.MustCompile("pull image error"),
}

func checkLogs(t *testing.T, str string) {
outer:
	for _, line := range strings.Split(str, "\n") {
		// Ignore lines that match any of the 'ignore patterns'.
		for _, regex := range ignoreRegexes {
			if regex.MatchString(line) {
				fmt.Printf("Ignoring line: %s\n", line)
				continue outer
			}
		}

		// "goroutine 0" is the main goroutine and is printed in stacktraces.
		if strings.Contains(line, "goroutine 0") {
			t.Error("Logs have a stack trace")
		}

		// The trailing open bracket is necessary to filter out false positives
		// in the log output. For example, as part of logging DNS requests, the
		// status string NOERROR is printed.
		if strings.Contains(line, "ERROR [") ||
			strings.Contains(line, "WARNING [") {
			t.Errorf("Logs have an error: %s", line)
		}

		// The minion logs should not contain any secret values. These secret
		// values were created in 70-secret-setup/secret_setup_test.go.
		if strings.Contains(line, "env secret") ||
			strings.Contains(line, "file secret") {
			t.Errorf("Logs have a secret value: %s", line)
		}
	}
}

func printKeldaPs() error {
	psout, err := exec.Command("kelda", "ps").CombinedOutput()
	fmt.Println(string(psout))
	return err
}
