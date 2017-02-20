package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client/getter"
	"github.com/quilt/quilt/db"

	log "github.com/Sirupsen/logrus"
)

func main() {
	clientGetter := getter.New()

	clnt, err := clientGetter.Client(api.DefaultSocket)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't get quiltctl client")
	}
	defer clnt.Close()

	leader, err := clientGetter.LeaderClient(clnt)
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't get leader client")
	}

	containers, err := leader.QueryContainers()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query containers")
	}

	connections, err := leader.QueryConnections()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query connections")
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query machines")
	}

	if test(machines, containers, connections) {
		log.Info("PASSED")
	} else {
		log.Info("FAILED")
	}
}

func test(machines []db.Machine, containers []db.Container,
	connections []db.Connection) bool {

	// Map of label to its publicly exposed ports.
	pubConns := map[string][]int{}
	for _, conn := range connections {
		if conn.From == "public" {
			for port := conn.MinPort; port <= conn.MaxPort; port++ {
				pubConns[conn.To] = append(pubConns[conn.To], port)
			}
		}
	}

	var failed bool
	mapper := newIPMapper(machines)
	for _, cont := range containers {
		contIP, err := mapper.containerIP(cont)
		if err != nil {
			log.Error(err)
			continue
		}
		for _, lbl := range cont.Labels {
			ports, ok := pubConns[lbl]
			if !ok {
				continue
			}
			for _, port := range ports {
				if !httpGetTest(contIP + ":" + strconv.Itoa(port)) {
					failed = true
				}
			}
		}
	}
	return !failed
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
		return "", fmt.Errorf("no public IP for %s", c.Minion)
	}
	return ip, nil
}

func httpGetTest(ip string) bool {
	log.Info("\n\n\nTesting ", ip)

	var failed bool
	for i := 0; i < 10; i++ {
		resp, err := http.Get("http://" + ip)
		if err != nil {
			log.WithError(err).Error("HTTP Error")
			failed = true
			continue
		}

		if resp.StatusCode == 200 {
			log.Info(resp)
		} else {
			log.Error(resp)
			failed = true
		}
	}

	return !failed
}
