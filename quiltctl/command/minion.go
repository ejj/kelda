// +build !windows

package command

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/minion"
	"github.com/quilt/quilt/version"

	log "github.com/Sirupsen/logrus"
)

// Minion contains the options for running the Quilt minion.
type Minion struct {
	role string
}

// NewMinionCommand creates a new Minion command instance.
func NewMinionCommand() *Minion {
	return &Minion{}
}

// InstallFlags sets up parsing for command line flags.
func (mCmd *Minion) InstallFlags(flags *flag.FlagSet) {
	flags.StringVar(&mCmd.role, "role", "", "the role of this quilt minion")

	flags.Usage = func() {
		fmt.Println("usage: quilt minion [-role=<role>]")
		fmt.Println("`role` defines the role of the quilt minion to run, e.g. " +
			"`Master` or `Worker`.")
		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the minion command.
func (mCmd *Minion) Parse(args []string) error {
	return nil
}

// Run starts the minion.
func (mCmd *Minion) Run() int {
	log.WithField("version", version.Version).Info("Starting Quilt minion")
	if err := mCmd.run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}
	return 0
}

func (mCmd *Minion) run() error {

	role, err := db.ParseRole(mCmd.role)

	if err != nil || role == db.None {
		return errors.New("no or improper role specified")
	}

	minion.Run(role)

	return nil
}
