package command

import (
	"flag"
	"fmt"
	"os"

	"github.com/NetSys/quilt/api"
	"github.com/NetSys/quilt/api/client"
	"github.com/NetSys/quilt/api/client/getter"
	"github.com/NetSys/quilt/db"

	log "github.com/Sirupsen/logrus"
)

// Ps contains the options for querying machines and containers.
type Ps struct {
	common       *commonFlags
	clientGetter client.Getter
}

// NewPsCommand creates a new Ps command instance.
func NewPsCommand() *Ps {
	return &Ps{
		common:       &commonFlags{},
		clientGetter: getter.New(),
	}
}

// InstallFlags sets up parsing for command line flags
func (pCmd *Ps) InstallFlags(flags *flag.FlagSet) {
	pCmd.common.InstallFlags(flags)
	flags.Usage = func() {
		fmt.Println("usage: quilt ps [-H=<daemon_host>]")
		fmt.Println("`ps` displays the status of quilt-managed " +
			"machines and containers.")

		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the ps command.
func (pCmd *Ps) Parse(args []string) error {
	return nil
}

// Run retrieves and prints all machines and containers.
func (pCmd *Ps) Run() int {
	if err := pCmd.run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	return 0
}

func (pCmd *Ps) run() error {
	localClient, err := pCmd.clientGetter.Client(pCmd.common.host)
	if err != nil {
		return fmt.Errorf("error connecting to quilt daemon: %s", err)
	}
	defer localClient.Close()

	var connections []db.Connection
	var containers []db.Container
	var machines []db.Machine

	connectionErr := make(chan error)
	containerErr := make(chan error)
	machineErr := make(chan error)

	go func() {
		machines, err = localClient.QueryMachines()
		machineErr <- err
	}()

	leaderClient, leadErr := pCmd.clientGetter.LeaderClient(localClient)
	if leadErr == nil {
		defer leaderClient.Close()

		go func() {
			connections, err = leaderClient.QueryConnections()
			connectionErr <- err
		}()

		go func() {
			containers, err = leaderClient.QueryContainers()
			containerErr <- err
		}()
	}

	if err := <-machineErr; err != nil {
		return fmt.Errorf("unable to query machines: %s", err)
	}

	fmt.Println("MACHINES")
	writeMachines(os.Stdout, machines)

	if leadErr != nil {
		log.WithError(leadErr).Debug("unable to connect to a cluster leader")
		return nil
	}
	if err := <-connectionErr; err != nil {
		return fmt.Errorf("unable to query connections: %s", err)
	}
	if err := <-containerErr; err != nil {
		return fmt.Errorf("unable to query containers: %s", err)
	}

	workerContainers := pCmd.queryWorkers(machines)
	containers = updateContainers(containers, workerContainers)

	fmt.Println("\nCONTAINERS")
	writeContainers(os.Stdout, containers, machines, connections)

	return nil
}

// queryWorkers gets a client for all connected worker machines (have a PublicIP
// and are role Worker) and returns a list of db.Container on these machines.
// If there is an error querying any machine, we skip it and attempt to return
// as many containers as possible.
func (pCmd *Ps) queryWorkers(machines []db.Machine) []db.Container {
	var workerMachines []db.Machine
	for _, m := range machines {
		if m.PublicIP != "" && m.Role == db.Worker {
			workerMachines = append(workerMachines, m)
		}
	}

	var workerContainers []db.Container
	numMachines := len(workerMachines)
	workerChannel := make(chan []db.Container, numMachines)
	for _, m := range workerMachines {
		client, err := pCmd.clientGetter.Client(api.RemoteAddress(m.PublicIP))
		if err != nil {
			numMachines = numMachines - 1
			continue
		}
		defer client.Close()

		go func() {
			qContainers, err := client.QueryContainers()
			if err != nil {
				log.WithError(err).
					Warn("QueryContainers on worker failed.")
			}
			workerChannel <- qContainers
		}()
	}

	for j := 0; j < numMachines; j++ {
		wc := <-workerChannel
		if wc == nil {
			continue
		}
		workerContainers = append(workerContainers, wc...)
	}
	return workerContainers
}

// updateContainers returns a list of containers from the hash join
// of the leader's view of containers and the workers' views of containers.
func updateContainers(lContainers []db.Container,
	wContainers []db.Container) []db.Container {

	if len(lContainers) == 0 {
		return wContainers
	}
	if len(wContainers) == 0 {
		return lContainers
	}

	var allContainers []db.Container

	// Map StitchID to db.Container for a hash join.
	cMap := make(map[string]db.Container)
	for _, wc := range wContainers {
		cMap[wc.StitchID] = wc
	}
	// If we see a leader container matching a worker container in the map, then
	// we use the container already in the map (worker container is fresher).
	// Otherwise, we add the leader container to our list.
	for _, lc := range lContainers {
		wc, ok := cMap[lc.StitchID]
		if ok {
			allContainers = append(allContainers, wc)
		} else {
			allContainers = append(allContainers, lc)
		}
	}
	return allContainers
}
