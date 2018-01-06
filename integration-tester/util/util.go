package util

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/api/client"
	"github.com/kelda/kelda/blueprint"
	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/connection"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/join"
	"github.com/kelda/kelda/util"
	"github.com/kelda/kelda/util/str"
)

// GetDefaultDaemonClient gets an API client connected to the daemon on the
// default socket with the default TLS credentials.
func GetDefaultDaemonClient() (client.Client, connection.Credentials, error) {
	creds, err := tlsIO.ReadCredentials(cliPath.DefaultTLSDir)
	if err != nil {
		return nil, nil, err
	}

	socket := os.Getenv("KELDA_HOST")
	if socket == "" {
		socket = api.DefaultSocket
	}

	c, err := client.New(socket, creds)
	if err != nil {
		return nil, nil, err
	}
	return c, creds, err
}

// CheckPublicConnections test that HTTP GETs against all ports that are
// connected to the public internet succeed.
func CheckPublicConnections(t *testing.T, machines []db.Machine,
	containers []db.Container, connections []db.Connection) {

	// Map of hostname to its publicly exposed ports.
	pubConns := map[string][]int{}
	for _, conn := range connections {
		if str.SliceContains(conn.From, "public") {
			for port := conn.MinPort; port <= conn.MaxPort; port++ {
				for _, to := range conn.To {
					pubConns[to] = append(pubConns[to], port)
				}
			}
		}
	}

	mapper := newIPMapper(machines)
	for _, cont := range containers {
		contIP, err := mapper.containerIP(cont)
		if err != nil {
			t.Error(err)
			continue
		}
		for _, port := range pubConns[cont.Hostname] {
			tryGet(t, contIP+":"+strconv.Itoa(port))
		}
	}
}

// WaitForContainers blocks until either all containers in the given blueprint
// have been booted, or 10 minutes have passed.
func WaitForContainers(bp blueprint.Blueprint) error {
	c, _, err := GetDefaultDaemonClient()
	if err != nil {
		return err
	}
	defer c.Close()

	return util.BackoffWaitFor(func() bool {
		curr, err := c.QueryContainers()
		if err != nil {
			return false
		}

		// Only match containers that have the same blueprint ID, and have been
		// booted.
		key := func(tgtIntf, actualIntf interface{}) int {
			tgt := tgtIntf.(blueprint.Container)
			actual := actualIntf.(db.Container)
			if tgt.ID == actual.BlueprintID && !actual.Created.IsZero() {
				return 0
			}
			return -1
		}
		_, unbooted, _ := join.Join(bp.Containers, curr, key)
		return len(unbooted) == 0
	}, 15*time.Second, 10*time.Minute)
}

// GetCurrentBlueprint returns the blueprint currently deployed by the daemon.
func GetCurrentBlueprint(c client.Client) (blueprint.Blueprint, error) {
	bps, err := c.QueryBlueprints()
	if err != nil {
		return blueprint.Blueprint{}, err
	}

	if len(bps) != 1 {
		return blueprint.Blueprint{}, errors.New(
			"unexpected number of blueprints")
	}
	return bps[0].Blueprint, nil
}

func tryGet(t *testing.T, ip string) {
	log.Info("\n\n\nTesting ", ip)

	for i := 0; i < 10; i++ {
		resp, err := http.Get("http://" + ip)
		if err != nil {
			t.Errorf("%s - HTTP GET error: %s", ip, err)
			continue
		}

		if resp.StatusCode != 200 {
			t.Errorf("%s - bad response code: %d", ip, resp.StatusCode)
		}
		log.Info(resp)
	}
}

type ipMapper map[string]string

func newIPMapper(machines []db.Machine) ipMapper {
	mapper := make(ipMapper)
	for _, m := range machines {
		mapper[m.PrivateIP] = m.PublicIP
	}
	return mapper
}

func (mapper ipMapper) containerIP(c db.Container) (string, error) {
	ip, ok := mapper[c.Minion]
	if !ok {
		return "", fmt.Errorf("no public IP for %v", c)
	}
	return ip, nil
}
