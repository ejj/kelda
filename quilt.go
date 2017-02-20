//go:generate protoc ./minion/pb/pb.proto --go_out=plugins=grpc:.
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	l_mod "log"
	"os"
	"strings"

	"github.com/quilt/quilt/quiltctl"
	"github.com/quilt/quilt/util"

	"google.golang.org/grpc/grpclog"

	log "github.com/Sirupsen/logrus"
)

func main() {
	flag.Usage = func() {
		fmt.Println("Usage: quilt " +
			"[-log-level=<level> | -l=<level>] [-H=<listen_address>] " +
			"[log-file=<log_output_file>] " +
			"[daemon | inspect <stitch> | run <stitch> | minion | " +
			"stop <namespace> | get <import_path> | " +
			"machines | containers | ps | ssh <id> [command] | " +
			"logs <container>]")
		fmt.Println("\nWhen provided a stitch, quilt takes responsibility\n" +
			"for deploying it as specified.  Alternatively, quilt may be\n" +
			"instructed to stop all deployments in a given namespace,\n" +
			"or the default namespace if none is provided.\n")
		flag.PrintDefaults()
		fmt.Println("        Valid logger levels are:\n" +
			"            debug, info, warn, error, fatal or panic.")
	}

	var logOut = flag.String("log-file", "", "log output file (will be overwritten)")
	var logLevel = flag.String("log-level", "info", "level to set logger to")
	flag.StringVar(logLevel, "l", "info", "level to set logger to")
	flag.Parse()

	level, err := parseLogLevel(*logLevel)
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

	// GRPC spews a lot of useless log messages so we tell to eat its logs, unless
	// we are in debug mode
	grpclog.SetLogger(l_mod.New(ioutil.Discard, "", 0))
	if level == log.DebugLevel {
		grpclog.SetLogger(log.StandardLogger())
	}

	if len(flag.Args()) == 0 {
		usage()
	}

	subcommand := flag.Arg(0)
	if quiltctl.HasSubcommand(subcommand) {
		quiltctl.Run(subcommand, flag.Args()[1:])
	} else {
		usage()
	}
}

func usage() {
	flag.Usage()
	os.Exit(1)
}

// parseLogLevel returns the log.Level type corresponding to the given string
// (case insensitive).
// If no such matching string is found, it returns log.InfoLevel (default) and an error.
func parseLogLevel(logLevel string) (log.Level, error) {
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
