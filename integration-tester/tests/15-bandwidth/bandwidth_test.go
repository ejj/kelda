package main

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

// The required bandwidth in Mb/s between two containers on different machines.
const requiredBandwidthIntermachine = 50.0

// The required bandwidth in Mb/s between two containers on the same machine.
const requiredBandwidthIntramachine = 2000.0

type testResult struct {
	client, server    db.Container
	iperfOutput       string
	bandwidthMbPerSec float64
	err               error
}

func TestBandwidth(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err.Error())
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err.Error())
	}

	// Group containers based on whether they share a machine with another container.
	// Containers that do not share a machine will connect to each other to test
	// inter-machine bandwidth.
	// Containers sharing a machine will connect to the other containers on the same
	// machine to test intra-machine bandwidth.
	minionToContainers := make(map[string][]db.Container)
	lonelyContainers := []db.Container{}
	groupedContainers := [][]db.Container{}

	for _, dbc := range containers {
		if dbc.Minion == "" {
			t.Fatalf("%v has not minion IP", dbc)
		}

		minionToContainers[dbc.Minion] = append(
			minionToContainers[dbc.Minion], dbc)
	}

	for _, containers := range minionToContainers {
		if len(containers) == 1 {
			lonelyContainers = append(lonelyContainers, containers[0])
		} else if len(containers) > 1 {
			groupedContainers = append(groupedContainers, containers)
		}
	}

	results := runTests(lonelyContainers)
	for _, group := range groupedContainers {
		results = append(results, runTests(group)...)
	}

	for _, res := range results {
		fmt.Printf("iperf output from %v to %v:\n", res.client, res.server)
		fmt.Println(res.iperfOutput)

		if res.err != nil {
			t.Errorf("%v to %v errored: %s", res.client, res.server, res.err)
			continue
		}

		required := requiredBandwidthIntermachine
		if res.client.Minion == res.server.Minion {
			required = requiredBandwidthIntramachine
		}

		if res.bandwidthMbPerSec < required {
			t.Errorf("bandwidth below minimum from %v to %v:\n"+
				"expected at least %f Mb/s, got %f Mb/s",
				res.client, res.server, required, res.bandwidthMbPerSec)
			continue
		}

		fmt.Printf("Average bandwidth: %f Mb/s\n\n", res.bandwidthMbPerSec)
	}
}

// runTests starts an iperf test from each container i to container i+1. The
// implementation assumes that an iperf server is listening on each container
// and that the containers are connected via port 5201.
func runTests(containers []db.Container) []testResult {
	wg := new(sync.WaitGroup)
	resultsChan := make(chan testResult, len(containers))

	for i, client := range containers {
		// The (len(containers) - 1)th container should connect to the 0th
		// container rather than the nonexistent (len(container))th container.
		server := containers[(i+1)%len(containers)]

		wg.Add(1)
		go func(client, server db.Container) {
			defer wg.Done()
			resultsChan <- test(client, server)
		}(client, server)
	}

	wg.Wait()
	close(resultsChan)

	results := []testResult{}
	for res := range resultsChan {
		results = append(results, res)
	}

	return results
}

func test(client, server db.Container) testResult {
	cmd := exec.Command("kelda", "ssh", client.BlueprintID,
		"iperf3", "-c", server.IP, "-f", "m", "-t", "30")
	outB, err := cmd.CombinedOutput()
	out := string(outB)

	res := testResult{
		client:      client,
		server:      server,
		iperfOutput: out,
		err:         err,
	}

	if err == nil {
		if bandwidth, parseErr := parseBandwidth(out); parseErr != nil {
			res.err = parseErr
			res.bandwidthMbPerSec = -1
		} else {
			res.bandwidthMbPerSec = bandwidth
		}
	}

	return res
}

func parseBandwidth(output string) (float64, error) {
	// For reference, here are the final six lines of a successful iperf3 test's
	// output:
	// [ ID] Interval           Transfer     Bandwidth       Retr
	// [  4]   0.00-30.00  sec   976 MBytes   273 Mbits/sec    0             sender
	// [  4]   0.00-30.00  sec   976 MBytes   273 Mbits/sec                  receiver
	//
	// iperf Done.
	//

	lines := strings.Split(output, "\n")
	if len(lines) < 6 {
		return -1, errors.New(
			"parsing error: expected at least 6 lines in output")
	}

	// Use receiver's reported bandwidth. We could instead choose the sender's
	// result, which is outputted one line before the receiver's result.
	receiverResult := lines[len(lines)-4]

	re := regexp.MustCompile(`(\d+) Mbits/sec`)
	match := re.FindStringSubmatch(receiverResult)

	if len(match) == 0 {
		return -1, fmt.Errorf("parsing error: couldn't find bandwidth in %q",
			receiverResult)
	}

	bandwidth, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return -1, fmt.Errorf("parsing error: %s", err)
	}

	return bandwidth, nil
}
