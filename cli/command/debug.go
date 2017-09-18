package command

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/cli/ssh"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/minion/supervisor/images"
	"github.com/kelda/kelda/util"
)

// Stored in variables to be mocked out for the unit tests.
var timestamp = time.Now
var execCmd = exec.Command

const (
	containerDir = "containers"
	machineDir   = "machines"
)

// Debug contains the options for downloading debug logs from machines and containers.
type Debug struct {
	privateKey string
	outPath    string
	all        bool
	containers bool
	machines   bool
	tar        bool
	ids        []string

	sshGetter ssh.Getter

	connectionHelper
}

type logTarget struct {
	ip   string
	dir  string
	id   string
	cmds []logCmd
}

type logCmd struct {
	name string
	cmd  string
}

var (
	// A list of commands to run on the daemon. These must be formatted with
	// the host address of the daemon. They will be prepended with 'quilt'.
	daemonCmds = []logCmd{
		{"ps", "ps -H=%s"},
		{"version", "version -H=%s"},
	}

	// A list of commands to output various machine logs.
	machineCmds = []logCmd{
		{"docker-ps", "docker ps -a"},
		{"minion", "docker logs minion"},
		{images.Etcd, "docker logs " + images.Etcd},
		{images.Ovsdb, "docker logs " + images.Ovsdb},
		{"syslog", "sudo cat /var/log/syslog"},
		{"journalctl", "sudo journalctl -xe"},
		{"uname", "uname -a"},
		{"dmesg", "dmesg"},
		{"uptime", "uptime"},
	}

	// A list of commands to output machine logs specific to Master machines.
	masterMachineCmds = []logCmd{
		{images.Ovnnorthd, "docker logs " + images.Ovnnorthd},
		{images.Registry, "docker logs " + images.Registry},
	}

	// A list of commands to output machine logs specific to Worker machines.
	workerMachineCmds = []logCmd{
		{images.Ovncontroller, "docker logs " + images.Ovncontroller},
		{images.Ovsvswitchd, "docker logs " + images.Ovsvswitchd},
	}

	// A list of commands to output various container logs. Container commands
	// need to be formatted with the DockerID.
	containerCmds = []logCmd{
		{"logs", "docker logs %s"},
		{"etc.tar", "docker cp %s:/etc -"},
	}
)

// NewDebugCommand creates a new Debug command instance.
func NewDebugCommand() *Debug {
	return &Debug{sshGetter: ssh.New}
}

var debugCommands = `quilt debug-logs [OPTIONS] [ID...]`

var debugExplanation = `Fetch logs for a set of machines or containers, placing
the contents in appropriately named files inside a timestamped tarball or folder.

To fetch debug logs from specific containers or machines, pass in the relevant
IDs. If no IDs are provided, either the -all, -containers, or -machines flag must be
set.

If -all is supplied, all other arguments are ignored. If -containers or
-machines are supplied, the list of IDs is ignored, but they do not override
each other. It follows that the below commands are equivalent:
quilt debug-logs -all
quilt debug-logs -machines -containers
quilt debug-logs <supply all machine/container IDs>

To get the logs of machine 09ed35808a0b using a specific private key:
quilt debug-logs -i ~/.ssh/quilt 09ed35808a0b`

// InstallFlags sets up parsing for command line flags.
func (dCmd *Debug) InstallFlags(flags *flag.FlagSet) {
	dCmd.connectionHelper.InstallFlags(flags)
	flags.StringVar(&dCmd.privateKey, "i", "",
		"path to the private key to use when connecting to the host")
	flags.StringVar(&dCmd.outPath, "o", "",
		"output path for the logs (defaults to timestamped path)")
	flags.BoolVar(&dCmd.all, "all", false, "if provided, fetch debug logs for all"+
		" machines and containers")
	flags.BoolVar(&dCmd.containers, "containers", false,
		"if provided, fetch all debug logs for application containers")
	flags.BoolVar(&dCmd.machines, "machines", false,
		"if provided, fetch all debug logs for machines"+
			" (including quilt system containers)")
	flags.BoolVar(&dCmd.tar, "tar", true,
		"if true (default), compress the logs into a tarball. If false, store"+
			" logs in a folder")

	flags.Usage = func() {
		util.PrintUsageString(debugCommands, debugExplanation, flags)
	}
}

// Parse parses the command line arguments for the debug command.
func (dCmd *Debug) Parse(args []string) error {
	dCmd.ids = args
	if len(dCmd.ids) == 0 && !(dCmd.all || dCmd.machines || dCmd.containers) {
		return errors.New("must supply at least one ID or set option")
	}

	return nil
}

// Run downloads debug logs from the relevant machines and containers.
func (dCmd Debug) Run() int {
	if dCmd.outPath == "" {
		dCmd.outPath = fmt.Sprintf("debug_logs_%s",
			timestamp().Format("Mon_Jan_02_15-04-05"))
	}
	if err := util.Mkdir(dCmd.outPath, 0755); err != nil {
		log.Error(err)
		return 1
	}

	machines, err := dCmd.client.QueryMachines()
	if err != nil {
		log.Error(err)
		return 1
	}

	ipMap := map[string]string{}
	for _, m := range machines {
		ipMap[m.PrivateIP] = m.PublicIP
	}

	containers, err := dCmd.client.QueryContainers()
	if err != nil {
		log.Error(err)
		return 1
	}

	dCmd.machines = dCmd.machines || dCmd.all
	dCmd.containers = dCmd.containers || dCmd.all

	var targets []logTarget
	mTargets := machinesToTargets(machines)
	cTargets := containersToTargets(containers, ipMap)
	if !(dCmd.machines || dCmd.containers) {
		targets = append(append(targets, cTargets...), mTargets...)
		if targets, err = filterTargets(targets, dCmd.ids); err != nil {
			log.Error(err)
			return 1
		}
	}

	if dCmd.machines {
		targets = append(targets, mTargets...)
	}
	if dCmd.containers {
		targets = append(targets, cTargets...)
	}

	return dCmd.downloadLogs(targets)
}

func (dCmd Debug) downloadLogs(targets []logTarget) int {
	rootDir := dCmd.outPath
	if err := util.Mkdir(filepath.Join(rootDir, machineDir), 0755); err != nil {
		log.Error(err)
		return 1
	}

	if err := util.Mkdir(filepath.Join(rootDir, containerDir), 0755); err != nil {
		log.Error(err)
		return 1
	}

	// Since we don't want the failure of downloading one or more logs to affect the
	// rest, errors that arise from the fetching of logs are ignored and errno is
	// simply incremented. The debug-logs command's exit code is errno if this line
	// of the code is reached before exiting.
	var errno int
	for _, cmd := range daemonCmds {
		file := filepath.Join(rootDir, cmd.name)
		fmtCmd := fmt.Sprintf(cmd.cmd, dCmd.host)
		qCmd := execCmd("quilt", strings.Fields(fmtCmd)...)
		log.Debugf("Gathering `quilt %s` output", fmtCmd)
		if result, err := qCmd.CombinedOutput(); err != nil {
			errno++
			log.Error(err)
		} else if err := util.WriteFile(file, result, 0644); err != nil {
			errno++
			log.Error(err)
		}
	}

	for _, t := range targets {
		path := filepath.Join(rootDir, t.dir, t.id)
		if err := util.Mkdir(path, 0755); err != nil {
			errno++
			log.Error(err)
			continue
		}

		conn, err := dCmd.sshGetter(t.ip, dCmd.privateKey)
		if err != nil {
			errno++
			log.Error(err)
			continue
		}

		for _, cmd := range t.cmds {
			log.Debugf("Downloading log '%s' for target %s", cmd.name,
				t.id)

			result, err := conn.CombinedOutput(cmd.cmd)
			if err != nil {
				log.WithError(err).WithField("output", string(result)).
					Errorf("Failed to get log '%s' from target %s",
						cmd.name, t.id)
				errno++
				continue
			}

			logFile := filepath.Join(path, cmd.name)
			if err := util.WriteFile(logFile, result, 0644); err != nil {
				errno++
				log.Error(err)
			}
		}
	}

	if errno > 0 {
		log.Error("Some downloads failed")
	}

	if dCmd.tar {
		errno += tarball(rootDir)
	}
	return errno
}

func machinesToTargets(machines []db.Machine) []logTarget {
	targets := []logTarget{}
	for _, m := range machines {
		if m.PublicIP == "" {
			continue
		}

		roleCmds := masterMachineCmds
		if m.Role == db.Worker {
			roleCmds = workerMachineCmds
		}

		t := logTarget{
			ip:   m.PublicIP,
			dir:  machineDir,
			id:   m.CloudID,
			cmds: append(machineCmds, roleCmds...),
		}
		targets = append(targets, t)
	}
	return targets
}

func containersToTargets(containers []db.Container, ips map[string]string) []logTarget {
	targets := []logTarget{}
	for _, c := range containers {
		if c.Minion == "" {
			continue
		}

		ip, ok := ips[c.Minion]
		if !ok {
			log.Errorf("No machine with private IP %s", c.Minion)
			continue
		}

		t := logTarget{
			ip:   ip,
			dir:  containerDir,
			id:   c.BlueprintID,
			cmds: nil,
		}
		for _, cmd := range containerCmds {
			cmd.cmd = fmt.Sprintf(cmd.cmd, c.DockerID)
			t.cmds = append(t.cmds, cmd)
		}
		targets = append(targets, t)
	}
	return targets
}

func filterTargets(targets []logTarget, ids []string) ([]logTarget, error) {
	result := []logTarget{}
	for _, id := range ids {
		t, err := findTarget(targets, id)
		if err != nil {
			return result, err
		}

		result = append(result, t)
	}
	return result, nil
}

func findTarget(targets []logTarget, id string) (logTarget, error) {
	choice := logTarget{id: ""}
	for _, t := range targets {
		if len(id) > len(t.id) || t.id[:len(id)] != id {
			continue
		}

		if choice.id != "" {
			return logTarget{}, fmt.Errorf("ambiguous ids %s and %s",
				choice.id, t.id)
		}
		choice = t
	}

	if choice.id == "" {
		return logTarget{}, fmt.Errorf("no target with id %s", id)
	}

	return choice, nil
}

func tarball(root string) int {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	ballUp := func(path string, info os.FileInfo, err error) error {
		hdr, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}
		hdr.Name = path

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		contents, err := util.ReadFile(path)
		if err != nil {
			return err
		}

		_, err = tw.Write([]byte(contents))
		return err
	}

	if err := util.Walk(root, ballUp); err != nil {
		log.WithError(err).Errorf("Failed to tarball directory %s", root)
		return 1
	}

	if err := tw.Close(); err != nil {
		log.WithError(err).Error("Failed to close tar writer")
		return 1
	}

	if err := util.RemoveAll(root); err != nil {
		log.WithError(err).Error("Failed to remove temporary log directory")
		return 1
	}

	if err := util.WriteFile(root+".tar", buf.Bytes(), 0644); err != nil {
		log.WithError(err).Error("Failed to write tarball")
		return 1
	}

	return 0
}
