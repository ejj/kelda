package command

import (
	"errors"
	"flag"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/api/util"
	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/supervisor"
	keldaUtil "github.com/kelda/kelda/util"
)

// SSH contains the options for SSHing into machines.
type SSH struct {
	target      string
	privateKey  string
	allocatePTY bool
	args        []string

	sshGetter ssh.Getter

	connectionHelper
}

// NewSSHCommand creates a new SSH command instance.
func NewSSHCommand() *SSH {
	return &SSH{sshGetter: ssh.New}
}

var sshCommands = "kelda ssh [OPTIONS] ID [COMMAND]"
var sshExplanation = `SSH into or execute a command in a machine or container.

If no command is supplied, a login shell is created.

To login to machine 09ed35808a0b with a specific private key:
kelda ssh -i ~/.ssh/kelda 09ed35808a0b

To run a command on container 8879fd2dbcee:
kelda ssh 8879fd2dbcee echo foo`

// InstallFlags sets up parsing for command line flags.
func (sCmd *SSH) InstallFlags(flags *flag.FlagSet) {
	sCmd.connectionHelper.InstallFlags(flags)
	flags.StringVar(&sCmd.privateKey, "i", "",
		"path to the private key to use when connecting to the host")
	flags.BoolVar(&sCmd.allocatePTY, "t", false,
		"attempt to allocate a pseudo-terminal")

	flags.Usage = func() {
		keldaUtil.PrintUsageString(sshCommands, sshExplanation, flags)
	}
}

// Parse parses the command line arguments for the ssh command.
func (sCmd *SSH) Parse(args []string) error {
	if len(args) == 0 {
		return errors.New("must specify a target")
	}

	sCmd.target = args[0]
	sCmd.args = args[1:]
	return nil
}

// Run SSHs into the given machine.
func (sCmd SSH) Run() int {
	allocatePTY := sCmd.allocatePTY || len(sCmd.args) == 0
	if allocatePTY && !isTerminal() {
		log.Error("Cannot allocate pseudo-terminal without a terminal")
		return 1
	}

	i, err := util.FuzzyLookup(sCmd.client, sCmd.target)
	if err != nil {
		log.WithError(err).Errorf("Failed to lookup %s", sCmd.target)
		return 1
	}

	cmd := strings.Join(sCmd.args, " ")
	shouldLogin := cmd == ""

	var cmdErr error
	switch t := i.(type) {
	case db.Machine:
		sshClient, err := sCmd.sshGetter(t.PublicIP, sCmd.privateKey)
		if err != nil {
			log.WithError(err).Error("Failed to set up SSH connection")
			return 1
		}
		defer sshClient.Close()

		if shouldLogin {
			cmdErr = sshClient.Shell()
		} else {
			cmdErr = sshClient.Run(sCmd.allocatePTY, cmd)
		}
	case db.Container:
		if t.PodName == "" {
			log.Error("Container not yet running")
			return 1
		}

		machines, err := sCmd.client.QueryMachines()
		if err != nil {
			log.WithError(err).Error("Failed to query machines")
			return 1
		}

		leaderIP, err := getLeaderIP(machines, sCmd.creds)
		if err != nil {
			log.WithError(err).Error("Failed to find leader machine")
			return 1
		}

		sshClient, err := sCmd.sshGetter(leaderIP, sCmd.privateKey)
		if err != nil {
			log.WithError(err).Error("Failed to set up SSH connection")
			return 1
		}
		defer sshClient.Close()

		if shouldLogin {
			sCmd.allocatePTY = true
			cmd = "sh"
		}
		cmdErr = containerExec(sshClient, t.PodName, sCmd.allocatePTY, cmd)
	default:
		panic("Not Reached")
	}

	if cmdErr != nil {
		if exitErr, ok := cmdErr.(exitError); ok {
			log.WithError(cmdErr).Debug(
				"SSH command returned a nonzero exit code")
			return exitErr.ExitStatus()
		}

		log.WithError(cmdErr).Error("Error running command")
		return 1
	}

	return 0
}

func containerExec(c ssh.Client, podName string, allocatePTY bool, cmd string) error {
	var flags string
	if allocatePTY {
		flags = "-it"
	}

	command := []string{
		"docker", "exec", flags, supervisor.KubeAPIServerName,
		"kubectl", "exec", flags, podName, "--", cmd,
	}

	return c.Run(allocatePTY, strings.Join(command, " "))
}

var getLeaderIP = client.GetLeaderIP
var isTerminal = func() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}

// exitError is an interface to "golang.org/x/crypto/ssh".ExitError that allows for
// mocking in unit tests.
type exitError interface {
	Error() string
	ExitStatus() int
}
