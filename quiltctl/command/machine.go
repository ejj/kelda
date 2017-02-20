package command

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	log "github.com/Sirupsen/logrus"

	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/util"
)

// Machine contains the options for querying machines.
type Machine struct {
	common       *commonFlags
	clientGetter client.Getter
}

// NewMachineCommand creates a new Machine command instance.
func NewMachineCommand() *Machine {
	return &Machine{
		common:       &commonFlags{},
		clientGetter: getter.New(),
	}
}

// InstallFlags sets up parsing for command line flags
func (mCmd *Machine) InstallFlags(flags *flag.FlagSet) {
	mCmd.common.InstallFlags(flags)
	flags.Usage = func() {
		fmt.Println("usage: quilt machine [-H=<daemon_host>]")
		fmt.Println("`machine` displays the status of quilt-managed machines.")
		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the machine command.
func (mCmd *Machine) Parse(args []string) error {
	return nil
}

// Run retrieves and prints the requested machines.
func (mCmd *Machine) Run() int {
	c, err := mCmd.clientGetter.Client(mCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer c.Close()

	machines, err := c.QueryMachines()
	if err != nil {
		log.WithError(err).Error("Unable to query machines.")
		return 1
	}

	writeMachines(os.Stdout, machines)
	return 0
}

func writeMachines(fd io.Writer, machines []db.Machine) {
	w := tabwriter.NewWriter(fd, 0, 0, 4, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "MACHINE\tROLE\tPROVIDER\tREGION\tSIZE\tPUBLIC IP\tCONNECTED")

	for _, m := range db.SortMachines(machines) {
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%v\n",
			util.ShortUUID(m.StitchID), m.Role, m.Provider, m.Region, m.Size,
			m.PublicIP, m.Connected)
	}
}
