package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

func TestEtcd(t *testing.T) {
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

	test(t, sshUtil, containers)
}

func test(t *testing.T, sshUtil util.SSHUtil, containers []db.Container) {
	var wg sync.WaitGroup
	for _, c := range containers {
		if !strings.Contains(c.Image, "etcd") {
			continue
		}

		wg.Add(1)
		go func(c db.Container) {
			defer wg.Done()
			out, err := sshUtil.SSH(c, "etcdctl", "cluster-health")
			if err != nil || !strings.Contains(out, "cluster is healthy") {
				t.Errorf("cluster is unhealthy when checked from %s "+
					"(%s): %s", c.Hostname, err, out)
			}
		}(c)
	}
	wg.Wait()
}
