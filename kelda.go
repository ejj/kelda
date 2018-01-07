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
	flags := flag.NewFlagSet("kelda", flag.ExitOnError)
	flags.Usage = func() {
		subcommands := strings.Join(cli.GetSubcommands(), ", ")
		subcommands = wordwrap.WrapString(subcommands, 78)
		subcommands = strings.Replace(subcommands, "\n", "\n  ", -1)

		explanation := keldaExplanation + "  " + subcommands
		util.PrintUsageString(keldaCommands, explanation, flags)
	}
	var logLevelInfo = "logging level (debug, info, warn, error, fatal, or panic)"
	var debugInfo = "turn on debug logging"

	var logOut = flags.String("log-file", "", "log output file (will be overwritten)")
	var logLevel = flags.String("log-level", "info", logLevelInfo)
	var debugOn = flags.Bool("verbose", false, debugInfo)
	flags.StringVar(logLevel, "l", "info", logLevelInfo)
	flags.BoolVar(debugOn, "v", false, debugInfo)
	flags.Parse(os.Args[1:])

	level, err := parseLogLevel(*logLevel, *debugOn)
	if err != nil {
		fmt.Println(err)
		flags.Usage()
		os.Exit(1)
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

	if len(flags.Args()) == 0 {
		flags.Usage()
		os.Exit(1)
	}

	subcommand := flags.Arg(0)
	if cli.HasSubcommand(subcommand) {
		cli.Run(subcommand, flags.Args()[1:])
	} else {
		flags.Usage()
		os.Exit(1)
	}
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
