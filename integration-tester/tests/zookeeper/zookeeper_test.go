package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"

	"github.com/satori/go.uuid"
)

func TestZookeeper(t *testing.T) {
	clnt, creds, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	sshUtil, err := util.NewSSHUtil(machines, creds)
	if err != nil {
		t.Fatalf("failed to create SSH util client: %s", err)
	}

	var zkContainers []db.Container
	for _, c := range containers {
		if strings.Contains(c.Image, "zookeeper") {
			zkContainers = append(zkContainers, c)
		}
	}

	test(t, sshUtil, zkContainers)
}

// Write a random key value pair to each zookeeper node, and then ensure that
// all nodes can retrieve all the written keys.
func test(t *testing.T, sshUtil util.SSHUtil, containers []db.Container) {
	var wg sync.WaitGroup
	expData := map[string]string{}
	for _, c := range containers {
		key := "/" + uuid.NewV4().String()
		val := uuid.NewV4().String()
		expData[key] = val

		wg.Add(1)
		go func(c db.Container) {
			defer wg.Done()

			out, err := sshUtil.SSH(c, "bin/zkCli.sh", "create", key, val)
			if err != nil {
				t.Errorf("unable to create key: %s", err)
				fmt.Printf("Failed to create key (%s): %s\n", err, out)
			}
		}(c)
	}
	wg.Wait()

	for _, c := range containers {
		for key, val := range expData {
			wg.Add(1)
			go func(c db.Container, key, val string) {
				defer wg.Done()

				out, err := sshUtil.SSH(c, "bin/zkCli.sh", "get", key)
				if err != nil || !strings.Contains(out, val) {
					t.Errorf("unexpected value: %s", err)
					fmt.Printf("%s: Key %s had wrong value.\n"+
						"Expected %s.\n"+
						"Error: %s.\n"+
						"Output: %s\n",
						c.Hostname, key, val, err, out)
				}
			}(c, key, val)
		}
	}
	wg.Wait()
}
