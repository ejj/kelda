package command

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/api/pb"
	apiUtil "github.com/kelda/kelda/api/util"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
)

const daemonTarget = "daemon"

var counterCommands = "kelda counters [OPTIONS] TARGET"
var counterExplanation = fmt.Sprintf(`Display internal counters tracked for
debugging purposes.  Most users will not need this command.

TARGET should be %q to retrieve the counters for the daemon. To retrieve the
counters for a machine, use the machine's ID as TARGET.`, daemonTarget)

// Counters implements the `kelda counters` command.
type Counters struct {
	target string

	connectionHelper
}

// InstallFlags sets up parsing for command line flags.
func (cmd *Counters) InstallFlags(flags *flag.FlagSet) {
	cmd.connectionHelper.InstallFlags(flags)
	flags.Usage = func() {
		util.PrintUsageString(counterCommands, counterExplanation, flags)
	}
}

// Parse parses the command line arguments for the counters command.
func (cmd *Counters) Parse(args []string) error {
	if len(args) == 0 {
		return errors.New("must specify a target")
	}
	cmd.target = args[0]
	return nil
}

// Run retrieves and prints all machines and containers.
func (cmd *Counters) Run() int {
	if err := cmd.run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	return 0
}

func (cmd *Counters) run() error {
	counters, err := queryCounters(cmd.client, cmd.target)
	if err != nil {
		return fmt.Errorf("error querying debug counters: %s", err)
	}

	printCounters(os.Stdout, counters)
	return nil
}

func queryCounters(c client.Client, tgt string) ([]pb.Counter, error) {
	if tgt == daemonTarget {
		return c.QueryCounters()
	}

	i, ip, err := apiUtil.FuzzyLookup(c, tgt)
	if err != nil {
		return nil, fmt.Errorf("resolve machine: %s", err)
	}

	if _, ok := i.(db.Machine); !ok {
		return nil, fmt.Errorf("could not find machine: %s", tgt)
	}

	return c.QueryMinionCounters(ip)
}

func printCounters(out io.Writer, counters []pb.Counter) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "COUNTER\tVALUE\tDELTA\n\t\t\n")

	byPkg := map[string][]pb.Counter{}
	for _, c := range counters {
		byPkg[c.Pkg] = append(byPkg[c.Pkg], c)
	}

	var packages []string
	for p := range byPkg {
		packages = append(packages, p)
	}
	sort.Strings(packages)

	for _, pkg := range packages {
		fmt.Fprintf(w, "%s\t\t\t\n", pkg)

		byName := map[string]pb.Counter{}
		for _, c := range byPkg[pkg] {
			byName[c.Name] = c
		}

		var names []string
		for n := range byName {
			names = append(names, n)
		}
		sort.Strings(names)

		for _, n := range names {
			val := byName[n].Value
			prev := byName[n].PrevValue
			fmt.Fprintf(w, "    %s\t%d\t%d\n", n, val, val-prev)
		}
	}
}
