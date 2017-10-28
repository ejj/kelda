package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/db"
)

type connectionTester struct {
	connectionMap map[string][]string
	allHostnames  []string
}

func newConnectionTester(clnt client.Client) (connectionTester, error) {
	loadBalancers, err := clnt.QueryLoadBalancers()
	if err != nil {
		return connectionTester{}, err
	}

	containers, err := clnt.QueryContainers()
	if err != nil {
		return connectionTester{}, err
	}

	connections, err := clnt.QueryConnections()
	if err != nil {
		return connectionTester{}, err
	}

	var allHostnames []string
	for _, lb := range loadBalancers {
		allHostnames = append(allHostnames, lb.Name+".q")
	}
	for _, c := range containers {
		allHostnames = append(allHostnames, c.Hostname+".q")
	}

	connectionMap := make(map[string][]string)
	for _, conn := range connections {
		connectionMap[conn.From] = append(connectionMap[conn.From], conn.To)
		// Connections are bi-directional.
		connectionMap[conn.To] = append(connectionMap[conn.To], conn.From)
	}

	return connectionTester{
		connectionMap: connectionMap,
		allHostnames:  allHostnames,
	}, nil
}

func (tester connectionTester) test(t *testing.T, container db.Container) {
	// We should be able to ping ourselves.
	expReachable := map[string]struct{}{
		container.Hostname + ".q": {},
	}
	for _, dst := range tester.connectionMap[container.Hostname] {
		expReachable[dst+".q"] = struct{}{}
	}

	var wg sync.WaitGroup

	test := func(hostname string) {
		defer wg.Done()
		_, err := keldaSSH(container, "ping", "-c", "3", "-W", "1", hostname)

		var errStr string
		reached := err == nil
		if _, ok := expReachable[hostname]; ok {
			if !reached {
				errStr = fmt.Sprintf("Failed to ping: %s %s -> %s. %s",
					time.Now(), container.BlueprintID, hostname, err)
			}
		} else if reached {
			errStr = fmt.Sprintf("Unexpected ping success: %s %s -> %s",
				time.Now(), container.BlueprintID, hostname)
		}

		if errStr != "" {
			fmt.Println(errStr)
			t.Error(errStr)
		}
	}

	for _, h := range tester.allHostnames {
		wg.Add(1)
		go test(h)
	}

	wg.Wait()
}
