package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/db"
	"github.com/quilt/quilt/join"
)

// anyIPAllowed is used to indicate that any non-error response is okay for an external
// DNS query.
const anyIPAllowed = "0.0.0.0"

var externalHostnames = []string{"google.com", "facebook.com", "en.wikipedia.org"}

type testResult struct {
	container        db.Container
	pingUnauthorized []string
	pingUnreachable  []string
	dnsIncorrect     []string
	dnsNotFound      []string
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
		if len(res.pingUnauthorized) != 0 {
			failed = true
			fmt.Println(".. FAILED, could ping unauthorized containers")
			for _, unauthorized := range res.pingUnauthorized {
				fmt.Printf(".... %s\n", unauthorized)
			}
		}
		if len(res.pingUnreachable) != 0 {
			failed = true
			fmt.Println(".. FAILED, couldn't ping authorized containers")
			for _, unreachable := range res.pingUnreachable {
				fmt.Printf(".... %s\n", unreachable)
			}
		}
		if len(res.dnsIncorrect) != 0 {
			failed = true
			fmt.Println(".. FAILED, hostnames resolved incorrectly")
			for _, incorrect := range res.dnsIncorrect {
				fmt.Printf(".... %s\n", incorrect)
			}
		}
		if len(res.dnsNotFound) != 0 {
			failed = true
			fmt.Println(".. FAILED, couldn't resolve hostnames")
			for _, notFound := range res.dnsNotFound {
				fmt.Printf(".... %s\n", notFound)
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
				testResultsChan <- tester.test(c)
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
	hostnameIPMap map[string]string
	allIPs        []string
	allHostnames  []string
}

func newNetworkTester(clnt client.Client) (networkTester, error) {
	labels, err := clnt.QueryLabels()
	if err != nil {
		return networkTester{}, err
	}

	hostnameIPMap := make(map[string]string)
	for _, host := range externalHostnames {
		hostnameIPMap[host] = anyIPAllowed
	}

	allIPsSet := make(map[string]struct{})
	labelMap := make(map[string]db.Label)
	for _, label := range labels {
		labelMap[label.Label] = label

		for _, ip := range append(label.ContainerIPs, label.IP) {
			allIPsSet[ip] = struct{}{}
		}

		hostnameIPMap[label.Label+".q"] = label.IP
		for i, ip := range label.ContainerIPs {
			hostnameIPMap[fmt.Sprintf("%d.%s.q", i+1, label.Label)] = ip
		}
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
		hostnameIPMap: hostnameIPMap,
		allIPs:        allIPs,
	}, nil
}

type pingResult struct {
	target    string
	reachable bool
}

// We have to limit our parallelization because each `quilt exec` creates a new SSH login
// session. Doing this quickly in parallel breaks system-logind
// on the remote machine: https://github.com/systemd/systemd/issues/2925.
const concurrencyLimit = 10

func (tester networkTester) pingAll(container db.Container) []pingResult {
	pingResultsChan := make(chan pingResult, len(tester.allIPs))

	// Create worker threads.
	pingRequests := make(chan string, concurrencyLimit)
	var wg sync.WaitGroup
	for i := 0; i < concurrencyLimit; i++ {
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

type lookupResult struct {
	hostname string
	ip       string
	err      error
}

// Resolve all hostnames on the container with the given StitchID. Parallelize
// over the hostnames.
func (tester networkTester) lookupAll(container db.Container) []lookupResult {
	lookupResultsChan := make(chan lookupResult, len(tester.hostnameIPMap))

	// Create worker threads.
	lookupRequests := make(chan string, concurrencyLimit)
	var wg sync.WaitGroup
	for i := 0; i < concurrencyLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hostname := range lookupRequests {
				ip, err := lookup(container.StitchID, hostname)
				lookupResultsChan <- lookupResult{hostname, ip, err}
			}
		}()
	}

	// Feed worker threads.
	for hostname := range tester.hostnameIPMap {
		lookupRequests <- hostname
	}
	close(lookupRequests)
	wg.Wait()
	close(lookupResultsChan)

	// Collect results.
	var results []lookupResult
	for res := range lookupResultsChan {
		results = append(results, res)
	}

	return results
}

func (tester networkTester) test(container db.Container) testResult {
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

	var pingUnreachable, pingUnauthorized []string
	for _, badIntf := range failures {
		bad := badIntf.(pingResult)
		if bad.reachable {
			pingUnreachable = append(pingUnreachable, bad.target)
		} else {
			pingUnauthorized = append(pingUnauthorized, bad.target)
		}
	}

	lookupResults := tester.lookupAll(container)

	var dnsIncorrect, dnsNotFound []string
	for _, l := range lookupResults {
		if l.err != nil {
			msg := fmt.Sprintf("%s: %s", l.hostname, l.err)
			dnsNotFound = append(dnsNotFound, msg)
			continue
		}

		expIP := tester.hostnameIPMap[l.hostname]
		if l.ip != expIP && expIP != anyIPAllowed {
			msg := fmt.Sprintf("%s => %s (expected %s)", l.hostname, l.ip,
				expIP)
			dnsIncorrect = append(dnsIncorrect, msg)
		}
	}

	return testResult{
		container:        container,
		pingUnreachable:  pingUnreachable,
		pingUnauthorized: pingUnauthorized,
		dnsIncorrect:     dnsIncorrect,
		dnsNotFound:      dnsNotFound,
	}
}

// ping `target` from within container `id` with 3 packets, with a timeout of
// 1 second for each packet.
func ping(id string, target string) (string, bool) {
	outBytes, err := exec.Command(
		"quilt", "ssh", id, "ping", "-c", "3", "-W", "1", target).
		CombinedOutput()
	return string(outBytes), err == nil
}

func lookup(id string, hostname string) (string, error) {
	outBytes, err := exec.Command(
		"quilt", "ssh", id, "getent", "hosts", hostname).
		CombinedOutput()
	if err != nil {
		return "", err
	}

	fields := strings.Fields(string(outBytes))
	if len(fields) < 2 {
		return "", fmt.Errorf("parse error: expected %q to have at "+
			"least 2 fields", fields)
	}

	return fields[0], nil
}

type pingSlice []pingResult

func (ps pingSlice) Get(ii int) interface{} {
	return ps[ii]
}

func (ps pingSlice) Len() int {
	return len(ps)
}
