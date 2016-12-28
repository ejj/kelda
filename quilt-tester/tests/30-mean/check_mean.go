package main

import (
	"net/http"
	"strings"

	"github.com/NetSys/quilt/api"
	"github.com/NetSys/quilt/api/client/getter"
	"github.com/NetSys/quilt/db"

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

	machines, err := clnt.QueryMachines()
	if err != nil {
		log.WithError(err).Fatal("FAILED, couldn't query machines")
	}

	httpGetTest(machines, containers)
	log.Info("PASSED")
}

func httpGetTest(machines []db.Machine, containers []db.Container) {
	log.Info("HTTP Get Test")

	minionIPMap := map[string]string{}
	for _, m := range machines {
		minionIPMap[m.PrivateIP] = m.PublicIP
	}

	var publicIPs []string
	for _, c := range containers {
		if strings.Contains(c.Image, "haproxy") {
			ip, ok := minionIPMap[c.Minion]
			if !ok {
				log.WithField("container", c).Fatal(
					"FAILED, HAProxy with no public IP")
			}
			publicIPs = append(publicIPs, ip)
		}
	}

	log.Info("Public IPs: ", publicIPs)
	if len(publicIPs) == 0 {
		log.Fatal("FAILED, Found no public IPs")
	}

	for i := 0; i < 25; i++ {
		for _, ip := range publicIPs {
			resp, err := http.Get("http://" + ip)
			if err != nil {
				log.WithError(err).Fatal("FAILED, HTTP error")
			}

			if resp.StatusCode != 200 {
				log.Fatal("FAILED, HAProxy had non 200 status", resp)
			}

			log.Info(resp)
		}
	}
}
