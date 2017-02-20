package main

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/db"
)

// The required bandwidth in Mb/s between two containers on different machines.
const requiredBandwidthIntermachine = 50

// The required bandwidth in Mb/s between two containers on the same machine.
const requiredBandwidthIntramachine = 2000

type testResult struct {
	client, server    db.Container
	iperfOutput       string
	bandwidthMbPerSec int
	err               error
}

func main() {
	clientGetter := getter.New()

	clnt, err := clientGetter.Client(api.DefaultSocket)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't get quiltctl client")
	}
	defer clnt.Close()

	leader, err := clientGetter.LeaderClient(clnt)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't get leader client")
	}

	containers, err := leader.QueryContainers()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query containers")
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
			log.WithError(err).Fatalf("FAILED, %s has no minion IP", dbc)
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

	var failed bool
	for _, res := range results {
		fmt.Println("client:\t", res.client)
		fmt.Println("server:\t", res.server)
		if res.err != nil {
			failed = true
			fmt.Println("FAILED, exited with error: ", res.err)
			fmt.Println("iperf output follows:")
			fmt.Println(res.iperfOutput)
			continue
		}

		required := requiredBandwidthIntermachine
		if res.client.Minion == res.server.Minion {
			required = requiredBandwidthIntramachine
		}

		if res.bandwidthMbPerSec < required {
			failed = true
			fmt.Printf("FAILED, expected at least %d Mb/s, got %d Mb/s\n",
				required, res.bandwidthMbPerSec)
			fmt.Println("iperf output follows:")
			fmt.Println(res.iperfOutput)
			continue
		}

		fmt.Printf("Average bandwidth: %d Mb/s\n\n", res.bandwidthMbPerSec)
	}

	if !failed {
		fmt.Println("PASSED")
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
	cmd := exec.Command("quilt", "ssh", client.StitchID,
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

func parseBandwidth(output string) (int, error) {
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

	bandwidth, err := strconv.Atoi(match[1])
	if err != nil {
		return -1, fmt.Errorf("parsing error: %s", err)
	}

	return bandwidth, nil
}
