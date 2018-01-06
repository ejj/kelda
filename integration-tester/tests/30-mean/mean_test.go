package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

// Response represents the JSON response from the kelda/mean-service
type Response struct {
	Text string `json:"text"`
	ID   string `json:"_id"`
	V    int    `json:"__v"`
}

func TestMean(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatal("couldn't get api client")
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatal("couldn't query containers")
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatal("couldn't query machines")
	}

	connections, err := clnt.QueryConnections()
	if err != nil {
		t.Fatal("couldn't query connections")
	}

	publicIPs := getPublicIPs(t, machines, containers)
	fmt.Printf("Public IPs: %s\n", publicIPs)
	if len(publicIPs) == 0 {
		t.Fatal("no public IPs")
	}

	logContainers(t, containers)
	httpPostTest(t, publicIPs)
	util.CheckPublicConnections(t, machines, containers, connections)
}

func logContainers(t *testing.T, containers []db.Container) {
	for _, c := range containers {
		out, err := exec.Command("kelda", "logs", c.BlueprintID).CombinedOutput()
		if err != nil {
			t.Errorf("Failed to log %s: %s", c, err)
			continue
		}
		fmt.Printf("Container: %s\n%s\n\n", c, string(out))
	}
}

func getPublicIPs(t *testing.T, machines []db.Machine,
	containers []db.Container) []string {
	minionIPMap := map[string]string{}
	for _, m := range machines {
		minionIPMap[m.PrivateIP] = m.PublicIP
	}
	var publicIPs []string
	for _, c := range containers {
		if strings.Contains(c.Image, "haproxy") {
			ip, ok := minionIPMap[c.Minion]
			if !ok {
				t.Fatalf("HAProxy with no public IP: %s", c.BlueprintID)
			}
			publicIPs = append(publicIPs, ip)
		}
	}

	return publicIPs
}

// checkInstances queries the todos for each instance and makes sure that all
// data is available from each instance.
func checkInstances(t *testing.T, publicIPs []string, expectedTodos int) {
	var todos []Response
	for _, ip := range publicIPs {
		endpoint := fmt.Sprintf("http://%s/api/todos", ip)
		resp, err := http.Get(endpoint)
		if err != nil {
			t.Errorf("%s - HTTP Error: %s", ip, err)
			continue
		}

		if resp.StatusCode != 200 {
			t.Errorf("%s - Bad response code: %d", ip, resp.StatusCode)
			fmt.Println(resp)
		}

		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&todos)
		if err != nil {
			t.Errorf("%s - JSON decoding error: %s", ip, err)
			continue
		}

		defer resp.Body.Close()
		if len(todos) != expectedTodos {
			t.Errorf("%s - Expected %d todos, but got %d", ip, expectedTodos,
				len(todos))
			continue
		}
	}
}

// httpPostTest tests that data persists across the kelda/mean-service.
// Data is POSTed to each instance, and then we check from all instances that
// all of the data can be recovered.
func httpPostTest(t *testing.T, publicIPs []string) {

	fmt.Println("HTTP Post Test")

	for i := 0; i < 10; i++ {
		for _, ip := range publicIPs {
			endpoint := fmt.Sprintf("http://%s/api/todos", ip)

			jsonStr := fmt.Sprintf("{\"text\": \"%s-%d\"}", ip, i)
			jsonBytes := bytes.NewBufferString(jsonStr)

			resp, err := http.Post(endpoint, "application/json", jsonBytes)
			if err != nil {
				t.Errorf("%s - HTTP Error: %s", ip, err)
				continue
			}

			if resp.StatusCode != 200 {
				t.Errorf("%s - Bad response code: %d",
					ip, resp.StatusCode)
				fmt.Println(resp)
			}
		}
	}

	checkInstances(t, publicIPs, 10*len(publicIPs))
}
