package util

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/supervisor"
)

// The maximum number of SSH sessions to concurrently allow to the leader.
const maxSSHSessions = 100

// SSHUtil makes it easy to parallelize executing commands on Kelda containers.
//
// It does several things to make executing commands faster:
// 1. It reuses SSH connections.
//
// 2. It caches state. Shelling out to `kelda ssh` requires requerying all
// machines and containers in order to figure out the leader's IP address.
//
// 3. It rate limits the number of parallel sessions to avoid overloading the
// remote host.
type SSHUtil struct {
	ssh.Client
	semaphore chan struct{}
}

// NewSSHUtil creates a new SSHUtil instance configured to connect to containers
// on the given machines. Any calls to `SSHUtil.SSH` for a container scheduled
// on a machine not given here will fail.
func NewSSHUtil(machines []db.Machine, creds connection.Credentials) (SSHUtil, error) {
	leaderIP, err := client.GetLeaderIP(machines, creds)
	if err != nil {
		return SSHUtil{}, fmt.Errorf("failed to get leader IP: %s", err)
	}

	semaphore := make(chan struct{}, maxSSHSessions)
	for i := 0; i < maxSSHSessions; i++ {
		semaphore <- struct{}{}
	}

	client, err := ssh.New(leaderIP, "")
	if err != nil {
		return SSHUtil{}, fmt.Errorf("failed to ssh to %s: %s",
			leaderIP, err)
	}

	return SSHUtil{Client: client, semaphore: semaphore}, nil
}

// SSH executes `cmd` on the given container, and returns the stdout and stderr
// output of the command in a single string.
func (sshUtil SSHUtil) SSH(dbc db.Container, containerCmd ...string) (string, error) {
	if dbc.PodName == "" {
		return "", errors.New("container not yet booted")
	}

	<-sshUtil.semaphore
	defer func() { sshUtil.semaphore <- struct{}{} }()

	containerCmdStr := strings.Join(containerCmd, " ")
	fmt.Println(dbc.Hostname, containerCmdStr)
	sshCmd := []string{
		"docker", "exec", supervisor.KubeAPIServerName,
		"kubectl", "exec", dbc.PodName, "--", containerCmdStr,
	}
	ret, err := sshUtil.CombinedOutput(strings.Join(sshCmd, " "))
	return string(ret), err
}
