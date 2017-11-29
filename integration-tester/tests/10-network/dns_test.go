package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

// anyIPAllowed is used to indicate that any non-error response is okay for an external
// DNS query.
const anyIPAllowed = "0.0.0.0"

var externalHostnames = []string{"google.com", "facebook.com", "en.wikipedia.org"}

func testDNS(t *testing.T, sshUtil util.SSHUtil, containers []db.Container,
	loadBalancers []db.LoadBalancer) {

	hostnameIPMap := make(map[string]string)
	for _, host := range externalHostnames {
		hostnameIPMap[host] = anyIPAllowed
	}

	for _, lb := range loadBalancers {
		hostnameIPMap[lb.Name+".q"] = lb.IP
	}

	for _, c := range containers {
		if c.Hostname != "" {
			hostnameIPMap[c.Hostname+".q"] = c.IP
		}
	}

	var wg sync.WaitGroup

	test := func(container db.Container, hostname string) {
		defer wg.Done()

		ip, err := lookup(sshUtil, container, hostname)

		expIP := hostnameIPMap[hostname]
		if err == nil && expIP != anyIPAllowed && ip != expIP {
			err = fmt.Errorf("wrong IP. expected %s, actual %s", expIP, ip)
		}

		if err != nil {
			errStr := fmt.Sprintf("DNS: %s %s -> %s: %s", time.Now(),
				container.BlueprintID, hostname, err)
			fmt.Println(errStr)
			t.Error(errStr)
		}
	}

	for _, container := range containers {
		for hostname := range hostnameIPMap {
			wg.Add(1)
			go test(container, hostname)
		}
	}

	wg.Wait()
}

func lookup(sshUtil util.SSHUtil, dbc db.Container, hostname string) (string, error) {
	stdout, err := sshUtil.SSH(dbc, "getent", "hosts", hostname)
	if err != nil {
		return "", err
	}

	fields := strings.Fields(stdout)
	if len(fields) < 2 {
		return "", fmt.Errorf("parse error: expected %q to have at "+
			"least 2 fields", fields)
	}

	return fields[0], nil
}
