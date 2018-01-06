package main

import (
	"strings"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

const (
	fetcherImage      = "alpine"
	loadBalancedLabel = "load-balanced"
)

func TestLoadBalancer(t *testing.T) {
	c, creds, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get local client: %s", err)
	}
	defer c.Close()

	containers, err := c.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't get containers: %s", err)
	}

	loadBalancers, err := c.QueryLoadBalancers()
	if err != nil {
		t.Fatalf("couldn't get load balancers: %s", err)
	}

	machines, err := c.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't get machines: %s", err)
	}

	sshUtil, err := util.NewSSHUtil(machines, creds)
	if err != nil {
		t.Fatalf("failed to create SSH util client: %s", err)
	}

	var fetcher *db.Container
	for _, c := range containers {
		if c.Image == fetcherImage {
			fetcherCopy := c
			fetcher = &fetcherCopy
			break
		}
	}

	if fetcher == nil {
		t.Fatal("couldn't find fetcher")
	}

	var loadBalancedContainers []string
	for _, lb := range loadBalancers {
		if lb.Name == loadBalancedLabel {
			loadBalancedContainers = lb.Hostnames
			break
		}
	}
	log.WithField("expected unique responses", len(loadBalancedContainers)).
		Info("Starting fetching..")

	loadBalancedCounts := map[string]int{}
	var loadBalancedCountsLock sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < len(loadBalancedContainers)*15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			output, err := sshUtil.SSH(*fetcher, "wget", "-q", "-O", "-",
				loadBalancedLabel)
			if err != nil {
				t.Errorf("Unable to GET: %s", err)
				log.WithError(err).WithField("output", output).
					Error("Unable to GET")
				return
			}

			loadBalancedCountsLock.Lock()
			loadBalancedCounts[strings.TrimSpace(output)]++
			loadBalancedCountsLock.Unlock()
		}()
	}
	wg.Wait()

	log.WithField("counts", loadBalancedCounts).Info("Fetching completed")
	if len(loadBalancedCounts) < len(loadBalancedContainers) {
		t.Fatalf("some containers not load balanced: "+
			"expected to query %d containers, got %d",
			len(loadBalancedContainers), len(loadBalancedCounts))
	}
}

func contains(lst []string, key string) bool {
	for _, v := range lst {
		if v == key {
			return true
		}
	}
	return false
}
