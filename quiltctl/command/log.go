package command

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/quiltctl/ssh"

	log "github.com/Sirupsen/logrus"
)

// Log is the structure for the `quilt logs` command.
type Log struct {
	privateKey     string
	sinceTimestamp string
	showTimestamps bool
	shouldTail     bool

	target string

	sshGetter    ssh.Getter
	clientGetter client.Getter
	common       *commonFlags
}

// NewLogCommand creates a new Log command instance.
func NewLogCommand() *Log {
	return &Log{
		sshGetter:    ssh.New,
		clientGetter: getter.New(),
		common:       &commonFlags{},
	}
}

var logsUsage = `usage: quilt logs [-H=<daemon_host>] [-i=<private_key>] <stitch_id>

Fetch the logs of a container or machine minion.
Either a container or machine ID can be supplied.

To get the logs of container 8879fd2dbcee with a specific private key:
quilt logs -i ~/.ssh/quilt 8879fd2dbcee

To follow the logs of the minion on machine 09ed35808a0b:
quilt logs -f 09ed35808a0b
`

// InstallFlags sets up parsing for command line flags.
func (lCmd *Log) InstallFlags(flags *flag.FlagSet) {
	lCmd.common.InstallFlags(flags)

	flags.StringVar(&lCmd.privateKey, "i", "",
		"the private key to use to connect to the host")
	flags.StringVar(&lCmd.sinceTimestamp, "since", "", "show logs since timestamp")
	flags.BoolVar(&lCmd.shouldTail, "f", false, "follow log output")
	flags.BoolVar(&lCmd.showTimestamps, "t", false, "show timestamps")

	flags.Usage = func() {
		fmt.Println(logsUsage)
		flags.PrintDefaults()
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
	c, err := lCmd.clientGetter.Client(lCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer c.Close()

	mach, machErr := getMachine(c, lCmd.target)
	contHost, cont, contErr := getContainer(c, lCmd.clientGetter, lCmd.target)

	resolvedMachine := machErr == nil
	resolvedContainer := contErr == nil

	switch {
	case !resolvedMachine && !resolvedContainer:
		log.WithFields(log.Fields{
			"machine error":   machErr.Error(),
			"container error": contErr.Error(),
		}).Error("Failed to resolve target machine or container")
		return 1
	case resolvedMachine && resolvedContainer:
		log.WithFields(log.Fields{
			"machine":   mach,
			"container": cont,
		}).Error("Ambiguous ID")
		return 1
	}

	if resolvedContainer && cont.DockerID == "" {
		log.Error("Container not yet running")
		return 1
	}

	cmd := []string{"docker", "logs"}
	if lCmd.sinceTimestamp != "" {
		cmd = append(cmd, fmt.Sprintf("--since=%s", lCmd.sinceTimestamp))
	}
	if lCmd.showTimestamps {
		cmd = append(cmd, "--timestamps")
	}
	if lCmd.shouldTail {
		cmd = append(cmd, "--follow")
	}

	host := contHost
	if resolvedMachine {
		host = mach.PublicIP
		cmd = append(cmd, "minion")
	} else {
		cmd = append(cmd, cont.DockerID)
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
