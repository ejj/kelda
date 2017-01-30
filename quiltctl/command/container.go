package command

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	log "github.com/Sirupsen/logrus"
	units "github.com/docker/go-units"

	"github.com/NetSys/quilt/api/client"
	"github.com/NetSys/quilt/api/client/getter"
	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/util"
)

// Container contains the options for querying containers.
type Container struct {
	common       *commonFlags
	clientGetter client.Getter
}

// NewContainerCommand creates a new Container command instance.
func NewContainerCommand() *Container {
	return &Container{
		clientGetter: getter.New(),
		common:       &commonFlags{},
	}
}

// InstallFlags sets up parsing for command line flags
func (cCmd *Container) InstallFlags(flags *flag.FlagSet) {
	cCmd.common.InstallFlags(flags)
	flags.Usage = func() {
		fmt.Println("usage: quilt container [-H=<daemon_host>]")
		fmt.Println("`container` displays the status of quilt-managed " +
			"Docker containers.")

		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the container command.
func (cCmd *Container) Parse(args []string) error {
	return nil
}

// Run retrieves and prints the requested containers.
func (cCmd *Container) Run() int {
	localClient, err := cCmd.clientGetter.Client(cCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer localClient.Close()

	c, err := cCmd.clientGetter.LeaderClient(localClient)
	if err != nil {
		log.WithError(err).Error("Error connecting to leader.")
		return 1
	}
	defer c.Close()

	containers, err := c.QueryContainers()
	if err != nil {
		log.WithError(err).Error("Unable to query containers.")
		return 1
	}

	connections, err := c.QueryConnections()
	if err != nil {
		log.WithError(err).Error("Unable to query connections.")
		return 1
	}

	machines, err := localClient.QueryMachines()
	if err != nil {
		log.WithError(err).Error("Unable to query machines")
		return 1
	}

	writeContainers(os.Stdout, containers, machines, connections)
	return 0
}

func writeContainers(fd io.Writer, containers []db.Container, machines []db.Machine,
	connections []db.Connection) {
	w := tabwriter.NewWriter(fd, 0, 0, 4, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "ID\tMACHINE\tCONTAINER\tLABELS\tSTATUS\tCREATED\tPUBLIC IP")

	labelPublicPortMap := map[string]string{}
	for _, c := range connections {
		if c.From != "public" {
			continue
		}

		if c.MinPort == c.MaxPort {
			labelPublicPortMap[c.To] = fmt.Sprintf("%d", c.MinPort)
		} else {
			labelPublicPortMap[c.To] = fmt.Sprintf(
				"%d-%d", c.MinPort, c.MaxPort)
		}
	}

	ipIDMap := map[string]int{}
	idMachineMap := map[int]db.Machine{}
	for _, m := range machines {
		ipIDMap[m.PrivateIP] = m.ID
		idMachineMap[m.ID] = m
	}

	machineDBC := map[int][]db.Container{}
	for _, dbc := range containers {
		id := ipIDMap[dbc.Minion]
		machineDBC[id] = append(machineDBC[id], dbc)
	}

	var machineIDs []int
	for key := range machineDBC {
		machineIDs = append(machineIDs, key)
	}
	sort.Ints(machineIDs)

	for i, machineID := range machineIDs {
		if i > 0 {
			// Insert a blank line between each machine.
			// Need to print tabs in a blank line; otherwise, spacing will
			// change in subsequent lines.
			fmt.Fprintf(w, "\t\t\t\t\t\t\n")
		}

		dbcs := machineDBC[machineID]
		sort.Sort(db.ContainerSlice(dbcs))
		for _, dbc := range dbcs {
			publicPorts := []string{}
			for _, label := range dbc.Labels {
				if p, ok := labelPublicPortMap[label]; ok {
					publicPorts = append(publicPorts, p)
				}
			}

			machine := machineStr(machineID)
			container := containerStr(dbc.Image, dbc.Command)
			labels := strings.Join(dbc.Labels, ", ")
			status := dbc.Status
			if dbc.Status == "" && dbc.Minion != "" {
				status = "scheduled"
			}
			created := ""
			if !dbc.Created.IsZero() {
				createdTime := dbc.Created.Local()
				duration := units.HumanDuration(time.Since(createdTime))
				created = fmt.Sprintf("%s ago", duration)
			}

			publicIP := publicIPStr(idMachineMap[machineID].PublicIP,
				publicPorts)

			fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
				util.ShortUUID(dbc.StitchID), machine, container, labels,
				status, created, publicIP)
		}
	}
}

func machineStr(machineID int) string {
	if machineID == 0 {
		return ""
	}
	return fmt.Sprintf("Machine-%d", machineID)
}
func containerStr(image string, args []string) string {
	if image == "" {
		return ""
	}
	return fmt.Sprintf("%s %s", image, strings.Join(args, " "))
}

func publicIPStr(hostPublicIP string, publicPorts []string) string {
	if hostPublicIP == "" || len(publicPorts) == 0 {
		return ""
	}

	if len(publicPorts) == 1 {
		return fmt.Sprintf("%s:%s", hostPublicIP, publicPorts[0])
	}

	return fmt.Sprintf("%s:[%s]", hostPublicIP, strings.Join(publicPorts, ","))
}
