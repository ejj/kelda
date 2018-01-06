package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/db"
	testerUtil "github.com/kelda/kelda/integration-tester/util"
	"github.com/kelda/kelda/util"
)

// stop stops the given namespace, and waits up to 5 minutes for the command to
// take effect.
func stop(namespace string) (string, string, error) {
	cmd := exec.Command("kelda", "stop", "-f", namespace)

	stdout, stderr, err := execCmd(cmd, "STOP", log.cmdLogger)
	if err != nil {
		return stdout, stderr, err
	}

	// Sleep 2 minutes to give the daemon ample time to do the first query of
	// the current machines in the cloud.
	time.Sleep(2 * time.Minute)

	// Now that we can trust that the machines returned by the daemon are
	// really the machines running in the cloud, block until there are not any
	// machines.
	noMachines := func() bool {
		machines, err := queryMachines()
		return err == nil && len(machines) == 0
	}
	err = util.BackoffWaitFor(noMachines, 15*time.Second, 3*time.Minute)
	return stdout, stderr, err
}

// npmInstall installs the npm dependencies in the current directory.
func npmInstall() (string, string, error) {
	cmd := exec.Command("npm", "install", ".")
	return execCmd(cmd, "NPM-INSTALL", log.cmdLogger)
}

// runBlueprint runs the given blueprint. Note that it does not block on the connection
// status of the machines.
func runBlueprint(blueprint string) (string, string, error) {
	cmd := exec.Command("kelda", "run", "-f", blueprint)
	return execCmd(cmd, "RUN", log.cmdLogger)
}

// runKeldaDaemon starts the daemon.
func runKeldaDaemon() {
	socket := os.Getenv("KELDA_HOST")
	if socket == "" {
		socket = api.DefaultSocket
	}
	os.Remove(socket[len("unix://"):])

	args := []string{"-l", "debug", "daemon"}
	cmd := exec.Command("kelda", args...)
	execCmd(cmd, "KELDA", log.daemonLogger)
}

func logAndUpdate(sc bufio.Scanner, l fileLogger, logFmt string) chan string {
	outputChan := make(chan string, 1)
	go func() {
		// This loop exits when the scanner reaches the end of input, which
		// happens when the command terminates. Thus, we don't need a channel
		// to force this thread to exit.
		var output string
		for sc.Scan() {
			line := sc.Text()
			output += line

			// Remove the newline if there is one because println
			// appends one automatically.
			logStr := strings.TrimSuffix(line, "\n")
			l.println(fmt.Sprintf(logFmt, logStr))
		}
		outputChan <- output
	}()
	return outputChan
}

// execCmd executes the given command, and returns the stdout and stderr output.
// `logLineTitle` is the prefix for logging to the container log.
func execCmd(cmd *exec.Cmd, logLineTitle string, l fileLogger) (string, string, error) {
	l.infoln(fmt.Sprintf("%s: Starting command: %v", logLineTitle, cmd.Args))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", err
	}

	// Save the command output while logging it.
	logFormatter := logLineTitle + " (%s): %%s"
	stdoutChan := logAndUpdate(*bufio.NewScanner(stdoutPipe), l,
		fmt.Sprintf(logFormatter, "stdout"))
	stderrChan := logAndUpdate(*bufio.NewScanner(stderrPipe), l,
		fmt.Sprintf(logFormatter, "stderr"))

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	stdout := <-stdoutChan
	stderr := <-stderrChan
	err = cmd.Wait()
	l.infoln(fmt.Sprintf("%s: Completed command: %v", logLineTitle, cmd.Args))
	return stdout, stderr, err
}

func queryMachines() ([]db.Machine, error) {
	c, _, err := testerUtil.GetDefaultDaemonClient()
	if err != nil {
		return []db.Machine{}, err
	}
	defer c.Close()

	return c.QueryMachines()
}
