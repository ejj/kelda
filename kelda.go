//go:generate protoc ./minion/pb/pb.proto --go_out=plugins=grpc:.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	l_mod "log"
	"os"
	"strings"

	"github.com/kelda/kelda/cli"
	"github.com/kelda/kelda/util"

	"google.golang.org/grpc/grpclog"

	"github.com/mitchellh/go-wordwrap"
	log "github.com/sirupsen/logrus"
)

var keldaCommands = "kelda [OPTIONS] COMMAND"

var keldaExplanation = `An approachable way to deploy to the cloud using Node.js.

To see the help text for a given command:
kelda COMMAND --help

Commands:
`

func main() {
	flag.Usage = printUsageString
	var logLevelInfo = "logging level (debug, info, warn, error, fatal, or panic)"
	var debugInfo = "turn on debug logging"

	var logOut = flag.String("log-file", "", "log output file (will be overwritten)")
	var logLevel = flag.String("log-level", "info", logLevelInfo)
	var debugOn = flag.Bool("verbose", false, debugInfo)
	flag.StringVar(logLevel, "l", "info", logLevelInfo)
	flag.BoolVar(debugOn, "v", false, debugInfo)
	flag.Parse()

	level, err := parseLogLevel(*logLevel, *debugOn)
	if err != nil {
		fmt.Println(err)
		usage()
	}
	log.SetLevel(level)
	log.SetFormatter(util.Formatter{})

	if *logOut != "" {
		file, err := os.Create(*logOut)
		if err != nil {
			fmt.Printf("Failed to create file %s\n", *logOut)
			os.Exit(1)
		}
		defer file.Close()
		log.SetOutput(file)
	}

	// GRPC spews a lot of useless log messages so we discard its logs.
	grpclog.SetLogger(l_mod.New(ioutil.Discard, "", 0))

	if len(flag.Args()) == 0 {
		usage()
	}

	subcommand := flag.Arg(0)
	if cli.HasSubcommand(subcommand) {
		cli.Run(subcommand, flag.Args()[1:])
	} else {
		usage()
	}
}

// printUsageString generates and prints a usage string based off of the
// subcommands defined in cli.go.
func printUsageString() {
	subcommands := strings.Join(cli.GetSubcommands(), ", ")
	subcommands = wordwrap.WrapString(subcommands, 78)
	subcommands = strings.Replace(subcommands, "\n", "\n  ", -1)

	explanation := keldaExplanation + "  " + subcommands
	util.PrintUsageString(keldaCommands, explanation, nil)
}

func usage() {
	flag.Usage()
	os.Exit(1)
}

// parseLogLevel returns the log.Level type corresponding to the given string
// (case insensitive).
// If no such matching string is found, it returns log.InfoLevel (default) and an error.
func parseLogLevel(logLevel string, debug bool) (log.Level, error) {
	if debug {
		return log.DebugLevel, nil
	}

	logLevel = strings.ToLower(logLevel)
	switch logLevel {
	case "debug":
		return log.DebugLevel, nil
	case "info":
		return log.InfoLevel, nil
	case "warn":
		return log.WarnLevel, nil
	case "error":
		return log.ErrorLevel, nil
	case "fatal":
		return log.FatalLevel, nil
	case "panic":
		return log.PanicLevel, nil
	}
	return log.InfoLevel, fmt.Errorf("bad log level: '%v'", logLevel)
}
