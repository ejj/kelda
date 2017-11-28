package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

func testPing(t *testing.T, sshUtil util.SSHUtil, containers []db.Container,
	loadBalancers []db.LoadBalancer, connections []db.Connection) {

	var allHostnames []string
	for _, lb := range loadBalancers {
		allHostnames = append(allHostnames, lb.Name+".q")
	}
	for _, c := range containers {
		allHostnames = append(allHostnames, c.Hostname+".q")
	}

	connectionMap := make(map[string][]string)
	for _, conn := range connections {
		for _, from := range conn.From {
			connectionMap[from] = append(connectionMap[from], conn.To...)
		}
	}

	var wg sync.WaitGroup
	for _, container := range containers {
		// We should be able to ping ourselves.
		expReachable := map[string]struct{}{container.Hostname + ".q": {}}
		for _, dst := range connectionMap[container.Hostname] {
			expReachable[dst+".q"] = struct{}{}
		}

		for _, hostname := range allHostnames {
			wg.Add(1)
			_, reachable := expReachable[hostname]
			container := container
			hostname := hostname
			go func() {
				defer wg.Done()
				out, err := ping(sshUtil, container, reachable,
					[]string{"ping", "-c", "3", "-W", "1"}, hostname)
				if err != nil {
					fmt.Printf("%s\n%s\n", err, out)
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()
}

func testHPing(t *testing.T, sshUtil util.SSHUtil, containers []db.Container,
	connections []db.Connection) {

	containerMap := map[string]db.Container{}
	for _, container := range containers {
		containerMap[container.Hostname] = container
	}

	var wg sync.WaitGroup
	test := func(container db.Container, hostname string, port int, reachable bool) {
		defer wg.Done()

		// The hping test sends a TCP SYN to the destination container on the
		// appropriate port.  If that container is listening, it will respond
		// with a SYN-ACK, and if it isn't, it will respond with a RST.  Either
		// way, we know that Kelda is allowing the communication.  If hping times
		// out, we know that the ACL is dropping our SYN.
		cmd := []string{"hping3", "-S", "-c", "3", "-p", fmt.Sprintf("%d", port)}
		out, err := ping(sshUtil, container, reachable, cmd, hostname)
		if err != nil {
			fmt.Printf("%s\n%s\n", err, out)
			t.Error(err)
		}
	}

	for _, conn := range connections {
		for _, from := range conn.From {
			container, ok := containerMap[from]
			if !ok {
				t.Errorf("Unknown container: %s", from)
				continue
			}

			for _, to := range conn.To {
				for port := conn.MinPort; port <= conn.MaxPort; port++ {
					wg.Add(1)
					go test(container, to, port, true)
				}
			}
		}
	}

	for _, container := range containerMap {
		for hostname := range containerMap {
			// Try a port in the ephemeral range that's unlikely to be
			// covered in a connection.  Could do something more
			// sophisticated to find an unused port later.
			wg.Add(1)
			go test(container, hostname, 50000, false)
		}
	}

	wg.Wait()
}

func ping(sshUtil util.SSHUtil, container db.Container, reachable bool,
	cmd []string, hostname string) (string, error) {
	cmd = append(cmd, hostname)
	out, err := sshUtil.SSH(container, cmd...)
	reached := err == nil
	if reachable {
		if !reached {
			return out, fmt.Errorf("unexpected failure: %s %s -> %s. %s",
				time.Now(), container.BlueprintID, hostname, err)
		}
	} else if reached {
		return out, fmt.Errorf("unexpected success: %s %s -> %s",
			time.Now(), container.BlueprintID, hostname)
	}
	return out, nil
}
