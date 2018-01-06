package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kelda/kelda/integration-tester/util"

	"github.com/stretchr/testify/assert"
)

func TestKibana(t *testing.T) {
	c, _, err := util.GetDefaultDaemonClient()
	assert.NoError(t, err)
	defer c.Close()

	containers, err := c.QueryContainers()
	assert.NoError(t, err)

	machines, err := c.QueryMachines()
	assert.NoError(t, err)

	var containerMinion string
	for _, dbc := range containers {
		if strings.Contains(dbc.Image, "kibana") {
			containerMinion = dbc.Minion
			break
		}
	}
	assert.NotEmpty(t, containerMinion, "failed to find container running Kibana")

	var publicIP string
	for _, dbm := range machines {
		if dbm.PrivateIP == containerMinion {
			publicIP = dbm.PublicIP
			break
		}
	}
	assert.NotEmpty(t, publicIP, "failed to find public IP for machine %s",
		containerMinion)

	addr := fmt.Sprintf("http://%s:5601/api/status", publicIP)
	fmt.Printf("Fetching %s\n", addr)
	resp, err := http.Get(addr)
	assert.NoError(t, err)

	var apiResp struct {
		Status struct {
			Overall struct {
				State string
			}
		}
	}
	decoder := json.NewDecoder(resp.Body)
	assert.NoError(t, decoder.Decode(&apiResp))

	assert.Equal(t, "green", apiResp.Status.Overall.State, "status should be green")
}
