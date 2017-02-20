package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/fatih/color"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/robertkrimen/otto"

	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/stitch"
)

// Run contains the options for running Stitches.
type Run struct {
	stitch string
	force  bool

	common       *commonFlags
	clientGetter client.Getter
}

// NewRunCommand creates a new Run command instance.
func NewRunCommand() *Run {
	return &Run{
		common:       &commonFlags{},
		clientGetter: getter.New(),
	}
}

// InstallFlags sets up parsing for command line flags.
func (rCmd *Run) InstallFlags(flags *flag.FlagSet) {
	rCmd.common.InstallFlags(flags)

	flags.StringVar(&rCmd.stitch, "stitch", "", "the stitch to run")
	flags.BoolVar(&rCmd.force, "f", false, "deploy without confirming changes")

	flags.Usage = func() {
		fmt.Println("usage: quilt run [-H=<daemon_host>] [-f] " +
			"[-stitch=<stitch>] <stitch>")
		fmt.Println("`run` compiles the provided stitch, and sends the " +
			"result to the Quilt daemon to be executed. Confirmation is " +
			"required if deploying the stitch would cause changes to an " +
			"existing cluster. Confirmation can be skipped with the " +
			"`-f` flag.")
		flags.PrintDefaults()
	}
}

// Parse parses the command line arguments for the run command.
func (rCmd *Run) Parse(args []string) error {
	if rCmd.stitch == "" {
		if len(args) == 0 {
			return errors.New("no spec specified")
		}
		rCmd.stitch = args[0]
	}

	return nil
}

var errNoCluster = errors.New("no cluster")

// Run starts the run for the provided Stitch.
func (rCmd *Run) Run() int {
	stitchPath := rCmd.stitch
	compiled, err := stitch.FromFile(stitchPath, stitch.DefaultImportGetter)
	if err != nil && os.IsNotExist(err) && !filepath.IsAbs(stitchPath) {
		// Automatically add the ".js" file suffix if it's not provided.
		if !strings.HasSuffix(stitchPath, ".js") {
			stitchPath += ".js"
		}
		compiled, err = stitch.FromFile(
			filepath.Join(stitch.GetQuiltPath(), stitchPath),
			stitch.DefaultImportGetter)
	}
	if err != nil {
		// Print the stacktrace if it's an Otto error.
		if ottoError, ok := err.(*otto.Error); ok {
			log.Error(ottoError.String())
		} else {
			log.Error(err)
		}
		return 1
	}
	deployment := compiled.String()

	c, err := rCmd.clientGetter.Client(rCmd.common.host)
	if err != nil {
		log.Error(err)
		return 1
	}
	defer c.Close()

	curr, err := getCurrentDeployment(c)
	if err != nil && err != errNoCluster {
		log.WithError(err).Error("Unable to get current deployment.")
		return 1
	}

	if !rCmd.force && err != errNoCluster {
		diff, err := diffDeployment(curr.String(), deployment)
		if err != nil {
			log.WithError(err).Error("Unable to diff deployments.")
			return 1
		}

		if diff == "" {
			fmt.Println("No change.")
		} else {
			fmt.Println(colorizeDiff(diff))
		}
		shouldDeploy, err := confirm(os.Stdin, "Continue with deployment?")
		if err != nil {
			log.WithError(err).Error("Unable to get user response.")
			return 1
		}

		if !shouldDeploy {
			fmt.Println("Deployment aborted by user.")
			return 0
		}
	}

	err = c.Deploy(deployment)
	if err != nil {
		log.WithError(err).Error("Error while starting run.")
		return 1
	}

	log.Debug("Successfully started run")
	return 0
}

func getCurrentDeployment(c client.Client) (stitch.Stitch, error) {
	clusters, err := c.QueryClusters()
	if err != nil {
		return stitch.Stitch{}, err
	}
	switch len(clusters) {
	case 0:
		return stitch.Stitch{}, errNoCluster
	case 1:
		return stitch.FromJSON(clusters[0].Spec)
	default:
		panic("unreached")
	}
}

func diffDeployment(currRaw, newRaw string) (string, error) {
	curr, err := prettifyJSON(currRaw)
	if err != nil {
		return "", err
	}
	new, err := prettifyJSON(newRaw)
	if err != nil {
		return "", err
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(curr),
		B:        difflib.SplitLines(new),
		FromFile: "Current",
		ToFile:   "Proposed",
		Context:  3,
	}
	return difflib.GetUnifiedDiffString(diff)
}

func prettifyJSON(toPrettify string) (string, error) {
	var prettified bytes.Buffer
	err := json.Indent(&prettified, []byte(toPrettify), "", "\t")
	if err != nil {
		return "", err
	}
	return prettified.String(), nil
}

func colorizeDiff(toColorize string) string {
	var colorized bytes.Buffer
	for _, line := range strings.SplitAfter(toColorize, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			colorized.WriteString(color.GreenString("%s", line))
		case strings.HasPrefix(line, "-"):
			colorized.WriteString(color.RedString("%s", line))
		default:
			colorized.WriteString(line)
		}
	}
	return colorized.String()
}

// Saved in a variable so that we can mock it for unit testing.
var confirm = func(in io.Reader, prompt string) (bool, error) {
	reader := bufio.NewReader(in)

	for {
		fmt.Printf("%s [y/n]: ", prompt)

		response, _, err := reader.ReadLine()
		if err != nil {
			return false, err
		}

		switch strings.ToLower(strings.TrimSpace(string(response))) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}
