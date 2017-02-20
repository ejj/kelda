package main

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"
)

// runSpecUntilConnected runs the given spec, and blocks until either all
// machines have connected back to the daemon, or 500 seconds have passed.
func runSpecUntilConnected(spec string) (string, string, error) {
	cmd := exec.Command("quilt", "run", "-f", spec)
	stdout, stderr, err := execCmd(cmd, "INFRA")
	if err != nil {
		return stdout, stderr, err
	}

	allMachinesConnected := func() bool {
		machines, err := queryMachines()
		if err != nil {
			return false
		}

		for _, m := range machines {
			if !m.Connected {
				return false
			}
		}

		return true
	}

	err = util.WaitFor(allMachinesConnected, 1*time.Second, 8*time.Minute)
	return stdout, stderr, err
}

// stop stops the given namespace, and blocks until there are no more machines
// in the namespace, or 2 minutes have passed.
func stop(namespace string) (string, string, error) {
	cmd := exec.Command("quilt", "stop", namespace)

	stdout, stderr, err := execCmd(cmd, "STOP")
	if err != nil {
		return stdout, stderr, err
	}

	stopped := func() bool {
		instances, err := getAWSInstances(namespace)
		if err != nil {
			log.testerLogger.println(
				fmt.Sprintf("Unable to get AWS instances: %s",
					err.Error()))
			return false
		}
		return len(instances) == 0
	}

	return stdout, stderr, util.WaitFor(stopped, 1*time.Second, 2*time.Minute)
}

// downloadSpecs gets the given import path.
func downloadSpecs(importPath string) (string, string, error) {
	cmd := exec.Command("quilt", "get", importPath)
	return execCmd(cmd, "GET")
}

// runSpec runs the given spec. Note that it does not block on the connection
// status of the machines.
func runSpec(spec string) (string, string, error) {
	cmd := exec.Command("quilt", "run", "-f", spec)
	return execCmd(cmd, "RUN")
}

// runQuiltDaemon starts the daemon.
func runQuiltDaemon() {
	cmd := exec.Command("quilt", "-l", "debug", "daemon")
	execCmd(cmd, "QUILT")
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
func execCmd(cmd *exec.Cmd, logLineTitle string) (string, string, error) {
	l := log.cmdLogger

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

func sshGen(host string, cmd *exec.Cmd) *exec.Cmd {
	script := "ssh"
	args := []string{"-o", "UserKnownHostsFile=/dev/null", "-o",
		"StrictHostKeyChecking=no", fmt.Sprintf("quilt@%s", host)}
	args = append(args, cmd.Args...)
	sshCmd := exec.Command(script, args...)
	return sshCmd
}

func scp(host string, source string, target string) error {
	cmd := exec.Command("scp", "-o", "UserKnownHostsFile=/dev/null", "-o",
		"StrictHostKeyChecking=no", source,
		fmt.Sprintf("quilt@%s:%s", host, target))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

func queryMachines() ([]db.Machine, error) {
	c, err := client.New(api.DefaultSocket)
	if err != nil {
		return []db.Machine{}, err
	}
	defer c.Close()

	return c.QueryMachines()
}

func getAWSInstances(namespace string) ([]*string, error) {
	// Find all of the instances under the namespace
	svc := ec2.New(session.New(), &aws.Config{Region: aws.String("us-west-1")})
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{Name: aws.String("network-interface.group-name"),
				Values: []*string{aws.String(namespace)}},
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		return []*string{}, err
	}

	// Build the list of InstanceIds
	ids := []*string{}
	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			ids = append(ids, inst.InstanceId)
		}
	}
	return ids, nil
}

func killAWS(namespace string) error {
	ids, err := getAWSInstances(namespace)
	if err != nil || len(ids) == 0 {
		return err
	}

	toDelete := &ec2.TerminateInstancesInput{
		InstanceIds: ids,
	}

	svc := ec2.New(session.New(), &aws.Config{Region: aws.String("us-west-1")})
	req, _ := svc.TerminateInstancesRequest(toDelete)
	return req.Send()
}

func cleanupMachines(namespace string) {
	l := log.testerLogger

	l.infoln("Cleaning up first with `quilt stop`.")
	if _, _, err := stop(namespace); err != nil {
		l.infoln("`quilt stop` errored.")
		l.errorln(err.Error())
		l.infoln("Now attempting to use killAWS.")
		if err := killAWS(namespace); err != nil {
			l.infoln("killAWS errored.")
			l.errorln(err.Error())
		}
	}
	l.infoln("Done cleaning up.")
}
