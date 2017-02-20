package command

import (
	"flag"

	"github.com/quilt/quilt/minion"
)

// Minion contains the options for running the Quilt minion.
type Minion struct{}

// InstallFlags sets up parsing for command line flags.
func (mCmd *Minion) InstallFlags(flags *flag.FlagSet) {
}

// Parse parses the command line arguments for the minion command.
func (mCmd *Minion) Parse(args []string) error {
	return nil
}

// Run starts the minion.
func (mCmd *Minion) Run() int {
	minion.Run()
	return 0
}
