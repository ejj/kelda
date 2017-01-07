package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/NetSys/quilt/api"
	"github.com/NetSys/quilt/api/client"
	"github.com/NetSys/quilt/api/client/getter"
	"github.com/NetSys/quilt/db"
	"github.com/NetSys/quilt/join"
)

type testResult struct {
	container    db.Container
	unauthorized []string
	unreachable  []string
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

	tester, err := newNetworkTester(leader)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't initialize network tester")
	}

	containers, err := leader.QueryContainers()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query containers")
	}

	var failed bool
	for _, res := range runTests(tester, containers) {
		fmt.Println(res.container)
		if len(res.unauthorized) != 0 {
			failed = true
			fmt.Println(".. FAILED, could ping unauthorized containers")
			for _, unauthorized := range res.unauthorized {
				fmt.Printf(".... %s\n", unauthorized)
			}
		}
		if len(res.unreachable) != 0 {
			failed = true
			fmt.Println(".. FAILED, couldn't ping authorized containers")
			for _, unreachable := range res.unreachable {
				fmt.Printf(".... %s\n", unreachable)
			}
		}
	}

	if !failed {
		fmt.Println("PASSED")
	}
}

// Gather test results for each container. For each minion machine, run one test
// at a time.
func runTests(tester networkTester, containers []db.Container) []testResult {
	// Create a separate test executor go routine for each minion machine.
	testChannels := make(map[string]chan db.Container)
	for _, c := range containers {
		testChannels[c.Minion] = make(chan db.Container)
	}

	var wg sync.WaitGroup
	testResultsChan := make(chan testResult, len(containers))
	for _, testChan := range testChannels {
		wg.Add(1)
		go func(testChan chan db.Container) {
			defer wg.Done()
			for c := range testChan {
				unreachable, unauthorized := tester.test(c)
				testResultsChan <- testResult{
					container:    c,
					unreachable:  unreachable,
					unauthorized: unauthorized,
				}
			}
		}(testChan)
	}

	// Feed the worker threads until we've run all the tests.
	for len(containers) != 0 {
		var remainingContainers []db.Container
		for _, c := range containers {
			select {
			case testChannels[c.Minion] <- c:
			default:
				remainingContainers = append(remainingContainers, c)
			}
		}
		containers = remainingContainers
		time.Sleep(1 * time.Second)
	}
	for _, testChan := range testChannels {
		close(testChan)
	}
	wg.Wait()
	close(testResultsChan)

	var testResults []testResult
	for res := range testResultsChan {
		testResults = append(testResults, res)
	}
	return testResults
}

type networkTester struct {
	labelMap      map[string]db.Label
	connectionMap map[string][]string
	allIPs        []string
}

func newNetworkTester(clnt client.Client) (networkTester, error) {
	labels, err := clnt.QueryLabels()
	if err != nil {
		return networkTester{}, err
	}

	allIPsSet := make(map[string]struct{})
	labelMap := make(map[string]db.Label)
	for _, label := range labels {
		labelMap[label.Label] = label
		for _, ip := range append(label.ContainerIPs, label.IP) {
			allIPsSet[ip] = struct{}{}
		}

		// XXX: We've temporarily disabled load balancing and thus the label.IP
		// isn't pingable.  When it's been re-implemented the following line (and
		// this comment) should be deleted.
		delete(allIPsSet, label.IP)
	}

	var allIPs []string
	for ip := range allIPsSet {
		allIPs = append(allIPs, ip)
	}

	connections, err := clnt.QueryConnections()
	if err != nil {
		return networkTester{}, err
	}

	connectionMap := make(map[string][]string)
	for _, conn := range connections {
		connectionMap[conn.From] = append(connectionMap[conn.From], conn.To)
		// Connections are bi-directional.
		connectionMap[conn.To] = append(connectionMap[conn.To], conn.From)
	}

	return networkTester{
		labelMap:      labelMap,
		connectionMap: connectionMap,
		allIPs:        allIPs,
	}, nil
}

type pingResult struct {
	target    string
	reachable bool
}

// We have to limit our parallelization because `ping` creates a new SSH login
// session every time. Doing this quickly in parallel breaks system-logind
// on the remote machine: https://github.com/systemd/systemd/issues/2925.
const pingConcurrencyLimit = 10

func (tester networkTester) pingAll(container db.Container) []pingResult {
	pingResultsChan := make(chan pingResult, len(tester.allIPs))

	// Create worker threads.
	pingRequests := make(chan string, pingConcurrencyLimit)
	var wg sync.WaitGroup
	for i := 0; i < pingConcurrencyLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range pingRequests {
				_, reachable := ping(container.StitchID, ip)
				pingResultsChan <- pingResult{
					target:    ip,
					reachable: reachable,
				}
			}
		}()
	}

	// Feed worker threads.
	for _, ip := range tester.allIPs {
		pingRequests <- ip
	}
	close(pingRequests)
	wg.Wait()
	close(pingResultsChan)

	// Collect results.
	var pingResults []pingResult
	for res := range pingResultsChan {
		pingResults = append(pingResults, res)
	}

	return pingResults
}

func (tester networkTester) test(container db.Container) (
	unreachable []string, unauthorized []string) {

	// We should be able to ping ourselves.
	expReachable := map[string]struct{}{
		container.IP: {},
	}
	for _, label := range container.Labels {
		for _, toLabelName := range tester.connectionMap[label] {
			toLabel := tester.labelMap[toLabelName]
			for _, ip := range append(toLabel.ContainerIPs, toLabel.IP) {
				expReachable[ip] = struct{}{}
			}
		}
		// We can ping our ovearching label, but not other containers within the
		// label. E.g. 1.yellow.q can ping yellow.q (but not 2.yellow.q).
		expReachable[tester.labelMap[label].IP] = struct{}{}
	}

	var expPings []pingResult
	for _, ip := range tester.allIPs {
		_, reachable := expReachable[ip]
		expPings = append(expPings, pingResult{
			target:    ip,
			reachable: reachable,
		})
	}
	pingResults := tester.pingAll(container)
	_, failures, _ := join.HashJoin(pingSlice(expPings), pingSlice(pingResults),
		nil, nil)

	for _, badIntf := range failures {
		bad := badIntf.(pingResult)
		if bad.reachable {
			unreachable = append(unreachable, bad.target)
		} else {
			unauthorized = append(unauthorized, bad.target)
		}
	}

	return unreachable, unauthorized
}

// ping `target` from within container `id` with 3 packets, with a timeout of
// 1 second for each packet.
func ping(id int, target string) (string, bool) {
	outBytes, err := exec.Command(
		"quilt", "exec", strconv.Itoa(id), "ping", "-c", "3", "-W", "1", target).
		CombinedOutput()
	return string(outBytes), err == nil
}

type pingSlice []pingResult

func (ps pingSlice) Get(ii int) interface{} {
	return ps[ii]
}

func (ps pingSlice) Len() int {
	return len(ps)
}
