package command

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/api/util"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/quiltctl/ssh"
)

// SSH contains the options for SSHing into machines.
type SSH struct {
	target      string
	privateKey  string
	allocatePTY bool
	args        []string

	common       *commonFlags
	clientGetter client.Getter
	sshGetter    ssh.Getter
}

// NewSSHCommand creates a new SSH command instance.
func NewSSHCommand() *SSH {
	return &SSH{
		clientGetter: getter.New(),
		sshGetter:    ssh.New,
		common:       &commonFlags{},
	}
}

var sshUsage = `usage: quilt ssh <id> [command]

Create a SSH session with the specified id.
Either a container or machine ID can be supplied.
If no command is supplied, a login shell is created.

To login to machine 09ed35808a0b with a specific private key:
quilt ssh -i ~/.ssh/quilt 09ed35808a0b

To run a command on container 8879fd2dbcee:
quilt ssh 8879fd2dbcee echo foo
`

// InstallFlags sets up parsing for command line flags.
func (sCmd *SSH) InstallFlags(flags *flag.FlagSet) {
	sCmd.common.InstallFlags(flags)
	flags.StringVar(&sCmd.privateKey, "i", "",
		"the private key to use to connect to the host")
	flags.BoolVar(&sCmd.allocatePTY, "t", false,
		"attempt to allocate a pseudo-terminal")

	flags.Usage = func() {
		fmt.Println(sshUsage)
		flags.PrintDefaults()
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

	c, err := sCmd.clientGetter.Client(sCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer c.Close()

	mach, machErr := getMachine(c, sCmd.target)
	contHost, cont, contErr := getContainer(c, sCmd.clientGetter, sCmd.target)

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

	host := contHost
	if resolvedMachine {
		host = mach.PublicIP
	}
	sshClient, err := sCmd.sshGetter(host, sCmd.privateKey)
	if err != nil {
		log.WithError(err).Error("Failed to setup SSH connection")
		return 1
	}
	defer sshClient.Close()

	cmd := strings.Join(sCmd.args, " ")
	shouldLogin := cmd == ""
	switch {
	case shouldLogin && resolvedMachine:
		err = sshClient.Shell()
	case !shouldLogin && resolvedMachine:
		err = sshClient.Run(sCmd.allocatePTY, cmd)
	case shouldLogin && resolvedContainer:
		err = containerExec(sshClient, cont.DockerID, true, "sh")
	case !shouldLogin && resolvedContainer:
		err = containerExec(sshClient, cont.DockerID, sCmd.allocatePTY, cmd)
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

func getMachine(c client.Client, id string) (db.Machine, error) {
	machines, err := c.QueryMachines()
	if err != nil {
		return db.Machine{}, err
	}

	var choice *db.Machine
	for _, m := range machines {
		if len(id) > len(m.StitchID) || m.StitchID[:len(id)] != id {
			continue
		}
		if choice != nil {
			return db.Machine{}, fmt.Errorf("ambiguous stitchIDs %s and %s",
				choice.StitchID, m.StitchID)
		}
		copy := m
		choice = &copy
	}

	if choice == nil {
		return db.Machine{}, fmt.Errorf("no machine with stitchID %q", id)
	}

	return *choice, nil
}

func getContainer(c client.Client, clientGetter client.Getter, id string) (
	host string, cont db.Container, err error) {

	containerClient, err := clientGetter.ContainerClient(c, id)
	if err != nil {
		return "", db.Container{}, err
	}
	defer containerClient.Close()

	container, err := util.GetContainer(containerClient, id)
	if err != nil {
		return "", db.Container{}, err
	}

	return containerClient.Host(), container, nil
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
