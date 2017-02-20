package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client/getter"
	log "github.com/Sirupsen/logrus"
)

var machineRegex = regexp.MustCompile(`Machine-(\d+){(.+?), .*, PublicIP=(.*?),`)

func main() {
	printQuiltPs()

	c, err := getter.New().Client(api.DefaultSocket)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't get quiltctl client")
	}
	defer c.Close()

	machines, err := c.QueryMachines()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query the machines")
	}

	failed := false
	for _, machine := range machines {
		fmt.Println(machine)
		logsOutput, err := exec.Command("quilt", "ssh", machine.StitchID,
			"sudo", "journalctl", "-o", "cat", "-u", "minion").
			CombinedOutput()
		if err != nil {
			log.WithError(err).Error("Unable to get minion logs")
			failed = true
			continue
		}
		outputStr := string(logsOutput)
		fmt.Println(outputStr)

		// "goroutine 0" is the main goroutine, and is thus always printed in
		// stacktraces.
		if strings.Contains(outputStr, "goroutine 0") ||
			strings.Contains(outputStr, "ERROR") ||
			strings.Contains(outputStr, "WARN") {
			failed = true
		}
	}

	if failed {
		fmt.Println("FAILED")
	} else {
		fmt.Println("PASSED")
	}
}

func printQuiltPs() {
	psout, err := exec.Command("quilt", "ps").CombinedOutput()
	if err != nil {
		log.WithError(err).Fatal("Failed to run `quilt ps`")
	}
	fmt.Println(string(psout))
}
