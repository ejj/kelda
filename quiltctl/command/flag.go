package command

import (
	"flag"

	"github.com/quilt/quilt/api"
)

type flagParser interface {
	// InstallFlags sets up parsing for command line flags.
	InstallFlags(*flag.FlagSet)
}

type commonFlags struct {
	host string
}

func (cf *commonFlags) InstallFlags(flags *flag.FlagSet) {
	flags.StringVar(&cf.host, "H", api.DefaultSocket, "the host to connect to")
}
