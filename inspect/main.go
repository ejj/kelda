package inspect

import (
	"fmt"
	"os"

	"github.com/quilt/quilt/stitch"
)

// Usage prints the usage string for the inspect tool.
func Usage() {
	fmt.Fprintln(
		os.Stderr,
		`quilt inspect is a tool that helps visualize Stitch specifications.
Usage: quilt inspect <path to spec file> <pdf|ascii|graphviz>
Dependencies
 - easy-graph (install Graph::Easy from cpan)
 - graphviz (install from your favorite package manager)`,
	)
}

// Main is the main function for inspect tool. Helps visualize stitches.
func Main(opts []string) int {
	if arglen := len(opts); arglen < 2 {
		fmt.Println("not enough arguments: ", arglen)
		Usage()
		return 1
	}

	configPath := opts[0]

	spec, err := stitch.FromFile(configPath, stitch.DefaultImportGetter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	graph, err := stitch.InitializeGraph(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	switch opts[1] {
	case "pdf", "ascii", "graphviz":
		viz(configPath, spec, graph, opts[1])
	default:
		Usage()
		return 1
	}

	return 0
}
