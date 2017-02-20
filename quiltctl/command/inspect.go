package command

import (
	"flag"
	"github.com/quilt/quilt/inspect"
)

// Inspect contains the options for inspecting Stitches.
type Inspect struct {
	args []string
}

// InstallFlags sets up parsing for command line flags.
func (iCmd *Inspect) InstallFlags(flags *flag.FlagSet) {
	flags.Usage = inspect.Usage
}

// Parse parses the command line arguments for the inspect command.
func (iCmd *Inspect) Parse(args []string) error {
	iCmd.args = args
	return nil
}

// Run inspects the provided Stitch.
func (iCmd *Inspect) Run() int {
	return inspect.Main(iCmd.args)
}
