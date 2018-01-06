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

	log "github.com/sirupsen/logrus"
)

const daemonTarget = "daemon"

var counterCommands = "kelda counters [OPTIONS] TARGETS"
var counterExplanation = fmt.Sprintf(`Display internal counters tracked for
developer debugging purposes.  Most users will not need this command.

TARGETS specifies the targets that counters should be fetched from, and can be
a machine's ID or %q for the daemon (the command fetches all counters tracked
on the given target).`, daemonTarget)

// Counters implements the `kelda counters` command.
type Counters struct {
	targets []string
	all     bool

	connectionHelper
}

// InstallFlags sets up parsing for command line flags.
func (cmd *Counters) InstallFlags(flags *flag.FlagSet) {
	cmd.connectionHelper.InstallFlags(flags)
	flags.BoolVar(&cmd.all, "all", false,
		"whether to fetch counters from all sources")
	flags.Usage = func() {
		util.PrintUsageString(counterCommands, counterExplanation, flags)
	}
}

// Parse parses the command line arguments for the counters command.
func (cmd *Counters) Parse(args []string) error {
	cmd.targets = args
	if len(cmd.targets) == 0 && !cmd.all {
		return errors.New("must specify a target")
	}
	return nil
}

// Run fetches and prints the desired counters.
func (cmd *Counters) Run() int {
	targets := cmd.targets
	if cmd.all {
		targets = []string{daemonTarget}
		machines, err := cmd.client.QueryMachines()
		if err == nil {
			for _, m := range machines {
				targets = append(targets, m.CloudID)
			}
		} else {
			log.WithError(err).Warn("Failed to query machines. " +
				"Only retrieving daemon counters.")
		}
	}

	var errorCount int
	for i, target := range targets {
		counters, err := queryCounters(cmd.client, target)
		if err != nil {
			log.WithError(err).WithField("target", target).
				Warn("Failed to query counters")
			errorCount++
			continue
		}

		if i != 0 {
			fmt.Println()
		}
		printCounters(os.Stdout, target, counters)
	}
	return errorCount
}

func queryCounters(c client.Client, tgt string) ([]pb.Counter, error) {
	if tgt == daemonTarget {
		return c.QueryCounters()
	}

	i, err := apiUtil.FuzzyLookup(c, tgt)
	if err != nil {
		return nil, fmt.Errorf("resolve machine: %s", err)
	}

	if _, ok := i.(db.Machine); !ok {
		return nil, fmt.Errorf("could not find machine: %s", tgt)
	}

	return c.QueryMinionCounters(i.(db.Machine).PublicIP)
}

func printCounters(out io.Writer, target string, counters []pb.Counter) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, target)
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
