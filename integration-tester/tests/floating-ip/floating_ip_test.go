package main

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

func TestFloatingIP(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	var floatingIP string
	for _, m := range machines {
		if m.Role == db.Worker && m.FloatingIP != "" {
			if floatingIP != "" {
				t.Fatalf("multiple workers with floating IPs: %s and %s",
					floatingIP, m.FloatingIP)
			}
			floatingIP = m.FloatingIP
		}
	}

	if floatingIP == "" {
		t.Fatal("no floating IP in the deployment")
	}

	url := "http://" + floatingIP
	fmt.Printf("Querying %s\n", url)
	resp, err := http.Get(url)
	fmt.Printf("Got: %v\n", resp)
	if err != nil {
		t.Fatalf("unable to retrieve %s: %s", url, err)
	}
}
