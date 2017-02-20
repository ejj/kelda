package command

import (
	"errors"
	"flag"
	"fmt"

	"github.com/quilt/quilt/stitch"

	log "github.com/Sirupsen/logrus"
	"github.com/robertkrimen/otto"
)

// Get contains the options for downloading imports.
type Get struct {
	importPath string
}

// InstallFlags sets up parsing for command line flags.
func (gCmd *Get) InstallFlags(flags *flag.FlagSet) {
	flags.StringVar(&gCmd.importPath, "import", "", "the stitch to download")

	flags.Usage = func() {
		fmt.Println("usage: quilt get [-import=<import>] <import> ")
		fmt.Printf("`get` downloads a given import into %s.\n",
			stitch.QuiltPathKey)
		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the get command.
func (gCmd *Get) Parse(args []string) error {
	if gCmd.importPath == "" {
		if len(args) == 0 {
			return errors.New("no import specified")
		}
		gCmd.importPath = args[0]
	}

	return nil
}

// Run downloads the requested import.
func (gCmd *Get) Run() int {
	if err := stitch.DefaultImportGetter.Get(gCmd.importPath); err != nil {
		// Print the stacktrace if it's an Otto error.
		if ottoError, ok := err.(*otto.Error); ok {
			log.Error(ottoError.String())
		}
		log.Errorf("Error getting import `%s`.", gCmd.importPath)
		return 1
	}

	fmt.Println("Successfully installed import.")

	return 0
}
