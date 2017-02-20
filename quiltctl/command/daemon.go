package command

import (
	"flag"
	"fmt"

	"github.com/quilt/quilt/api/server"
	"github.com/quilt/quilt/cluster"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/engine"
)

// Daemon contains the options for running the Quilt daemon.
type Daemon struct {
	common *commonFlags
}

// NewDaemonCommand creates a new Daemon command instance.
func NewDaemonCommand() *Daemon {
	return &Daemon{
		common: &commonFlags{},
	}
}

// InstallFlags sets up parsing for command line flags
func (dCmd *Daemon) InstallFlags(flags *flag.FlagSet) {
	dCmd.common.InstallFlags(flags)
	flags.Usage = func() {
		fmt.Println("usage: quilt daemon [-H=<daemon_host>]")
		fmt.Println("`daemon` starts the quilt daemon, which listens for" +
			"quilt API requests")

		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the daemon command.
func (dCmd *Daemon) Parse(args []string) error {
	return nil
}

// Run starts the daemon.
func (dCmd *Daemon) Run() int {
	conn := db.New()
	go engine.Run(conn)
	go server.Run(conn, dCmd.common.host)
	cluster.Run(conn)
	return 0
}
