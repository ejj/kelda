package command

import (
	"errors"
	"flag"
	"strings"

	apiUtil "github.com/kelda/kelda/api/util"
	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"

	log "github.com/sirupsen/logrus"
)

// Log is the structure for the `kelda logs` command.
type Log struct {
	privateKey string
	shouldTail bool

	target string

	sshGetter ssh.Getter

	connectionHelper
}

// NewLogCommand creates a new Log command instance.
func NewLogCommand() *Log {
	return &Log{sshGetter: ssh.New}
}

var logCommands = `kelda logs [OPTIONS] ID`
var logExplanation = `Fetch the logs of a container or machine minion. Either a container
or machine ID must be supplied.

To get the logs of container 8879fd2dbcee with a specific private key:
kelda logs -i ~/.ssh/kelda 8879fd2dbcee

To follow the logs of the minion on machine 09ed35808a0b:
kelda logs -f 09ed35808a0b`

// InstallFlags sets up parsing for command line flags.
func (lCmd *Log) InstallFlags(flags *flag.FlagSet) {
	lCmd.connectionHelper.InstallFlags(flags)

	flags.StringVar(&lCmd.privateKey, "i", "",
		"path to the private key to use when connecting to the host")
	flags.BoolVar(&lCmd.shouldTail, "f", false, "follow log output")

	flags.Usage = func() {
		util.PrintUsageString(logCommands, logExplanation, flags)
	}
}

// Parse parses the command line arguments for the `logs` command.
func (lCmd *Log) Parse(args []string) error {
	if len(args) == 0 {
		return errors.New("must specify a target container or machine")
	}

	lCmd.target = args[0]
	return nil
}

// Run finds the target container or machine minion and outputs logs.
func (lCmd *Log) Run() int {
	i, host, err := apiUtil.FuzzyLookup(lCmd.client, lCmd.target)
	if err != nil {
		log.WithError(err).Errorf("Failed to lookup %s", lCmd.target)
		return 1
	}

	cmd := []string{"docker", "logs"}
	if lCmd.shouldTail {
		cmd = append(cmd, "--follow")
	}

	switch t := i.(type) {
	case db.Machine:
		cmd = append(cmd, "minion")
	case db.Container:
		if t.DockerID == "" {
			log.Error("Container not yet running")
			return 1
		}
		cmd = append(cmd, t.DockerID)
	default:
		panic("Not Reached")
	}

	sshClient, err := lCmd.sshGetter(host, lCmd.privateKey)
	if err != nil {
		log.WithError(err).Info("Error opening SSH connection")
		return 1
	}
	defer sshClient.Close()

	if err = sshClient.Run(false, strings.Join(cmd, " ")); err != nil {
		log.WithError(err).Info("Error running command over SSH")
		return 1
	}

	return 0
}
