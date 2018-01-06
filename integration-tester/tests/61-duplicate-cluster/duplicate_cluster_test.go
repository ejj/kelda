package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/kelda/kelda/integration-tester/util"
)

var connectionRegex = regexp.MustCompile(`Registering worker (\d+\.\d+\.\d+\.\d+:\d+)`)

func TestDuplicateCluster(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	psPretty, err := exec.Command("kelda", "ps").Output()
	if err != nil {
		t.Fatalf("`kelda ps` failed: %s", err)
	}
	fmt.Println("`kelda ps` output:")
	fmt.Println(string(psPretty))

	var masters []string
	totalWorkers := 0
	for _, dbc := range containers {
		if strings.HasPrefix(dbc.Hostname, "spark-master") {
			id := dbc.BlueprintID
			masters = append(masters, id)
		} else if strings.HasPrefix(dbc.Hostname, "spark-worker") {
			totalWorkers++
		}
	}
	if len(masters) != 2 {
		t.Fatalf("Expected 2 masters: %+v", masters)
	}

	for _, master := range masters {
		logs, err := exec.Command("kelda", "logs", master).CombinedOutput()
		if err != nil {
			t.Fatalf("unable to get Spark master logs: %s", err)
		}

		// Each cluster's workers should connect only to its own master.
		logsStr := string(logs)
		workerSet := map[string]struct{}{}
		connectionMatches := connectionRegex.FindAllStringSubmatch(logsStr, -1)
		for _, wkMatch := range connectionMatches {
			workerSet[wkMatch[1]] = struct{}{}
		}
		if workerCount := len(workerSet); workerCount != totalWorkers/2 {
			t.Fatalf("wrong number of workers connected to master %s: "+
				"expected %d, got %d",
				master, totalWorkers/2, workerCount)
		}

		fmt.Printf("`kelda logs %s` output:\n", master)
		fmt.Println(logsStr)
	}
}
