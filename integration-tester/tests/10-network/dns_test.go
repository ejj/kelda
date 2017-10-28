package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/db"
)

// anyIPAllowed is used to indicate that any non-error response is okay for an external
// DNS query.
const anyIPAllowed = "0.0.0.0"

var externalHostnames = []string{"google.com", "facebook.com", "en.wikipedia.org"}

type dnsTester struct {
	hostnameIPMap map[string]string
}

func newDNSTester(clnt client.Client) (dnsTester, error) {
	loadBalancers, err := clnt.QueryLoadBalancers()
	if err != nil {
		return dnsTester{}, err
	}

	containers, err := clnt.QueryContainers()
	if err != nil {
		return dnsTester{}, err
	}

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

	return dnsTester{hostnameIPMap}, nil
}

func (tester dnsTester) test(t *testing.T, container db.Container) {
	var wg sync.WaitGroup

	test := func(hostname string) {
		defer wg.Done()

		ip, err := lookup(container, hostname)

		expIP := tester.hostnameIPMap[hostname]
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

	for h := range tester.hostnameIPMap {
		wg.Add(1)
		go test(h)
	}

	wg.Wait()
}

func lookup(dbc db.Container, hostname string) (string, error) {
	stdout, err := keldaSSH(dbc, "getent", "hosts", hostname)
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
