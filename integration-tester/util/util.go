package util

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/api/client"
	cliPath "github.com/kelda/kelda/cli/path"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util/str"
)

// GetDefaultDaemonClient gets an API client connected to the daemon on the
// default socket with the default TLS credentials.
func GetDefaultDaemonClient() (client.Client, error) {
	creds, err := tlsIO.ReadCredentials(cliPath.DefaultTLSDir)
	if err != nil {
		return nil, err
	}

	return client.New(api.DefaultSocket, creds)
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
