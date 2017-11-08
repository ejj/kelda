package command

import (
	"errors"
	"flag"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/kelda/kelda/api/util"
	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/db"
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

	i, host, err := util.FuzzyLookup(sCmd.client, sCmd.target)
	if err != nil {
		log.WithError(err).Errorf("Failed to lookup %s", sCmd.target)
		return 1
	}

	sshClient, err := sCmd.sshGetter(host, sCmd.privateKey)
	if err != nil {
		log.WithError(err).Error("Failed to set up SSH connection")
		return 1
	}
	defer sshClient.Close()

	cmd := strings.Join(sCmd.args, " ")
	shouldLogin := cmd == ""

	switch t := i.(type) {
	case db.Machine:
		if shouldLogin {
			err = sshClient.Shell()
		} else {
			err = sshClient.Run(sCmd.allocatePTY, cmd)
		}
	case db.Container:
		if t.DockerID == "" {
			log.Error("Container not yet running")
			return 1
		}

		if shouldLogin {
			err = containerExec(sshClient, t.DockerID, true, "sh")
		} else {
			err = containerExec(sshClient, t.DockerID, sCmd.allocatePTY, cmd)
		}
	default:
		panic("Not Reached")
	}

	if err != nil {
		if exitErr, ok := err.(exitError); ok {
			log.WithError(err).Debug(
				"SSH command returned a nonzero exit code")
			return exitErr.ExitStatus()
		}

		log.WithError(err).Error("Error running command")
		return 1
	}

	return 0
}

func containerExec(c ssh.Client, dockerID string, allocatePTY bool, cmd string) error {
	var flags string
	if allocatePTY {
		flags = "-it"
	}

	command := strings.Join([]string{"docker exec", flags, dockerID, cmd}, " ")
	return c.Run(allocatePTY, command)
}

var isTerminal = func() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}

// exitError is an interface to "golang.org/x/crypto/ssh".ExitError that allows for
// mocking in unit tests.
type exitError interface {
	Error() string
	ExitStatus() int
}
