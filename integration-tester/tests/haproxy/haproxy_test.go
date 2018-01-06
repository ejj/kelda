package main

import (
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const cookieName string = "SERVERID"

func TestURLrouting(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
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

	HAProxyIPs := getHAproxyIPs(t, machines, containers)
	if len(HAProxyIPs) == 0 {
		t.Fatal("Found no public IPs for HAProxy")
	}
	log.Info("Public proxy IPs: ", HAProxyIPs)

	expectedA := map[string]struct{}{
		"a1": {},
		"a2": {},
	}

	expectedB := map[string]struct{}{
		"b1": {},
		"b2": {},
		"b3": {},
	}

	httpGetTest(t, "serviceA.com", HAProxyIPs, expectedA)
	httpGetTest(t, "serviceB.com", HAProxyIPs, expectedB)
	cookieTest(t, "serviceA.com", HAProxyIPs, expectedA)
	cookieTest(t, "serviceB.com", HAProxyIPs, expectedB)
	badDomainTest(t, "badDomain.com", HAProxyIPs)
}

func httpGetTest(t *testing.T, domain string, HAProxyIPs []string,
	expected map[string]struct{}) {

	log.Info("Http GET test for ", domain)
	client := &http.Client{}
	checkResponses(t, domain, HAProxyIPs, expected, "", client)
}

func cookieTest(t *testing.T, domain string, HAProxyIPs []string,
	expected map[string]struct{}) {

	log.Info("Cookie test for ", domain)
	client := &http.Client{}

	// We just need a cookie from some proxy.
	resp, err := makeRequest(domain, HAProxyIPs[0], client, "")
	if err != nil {
		t.Errorf("failed to request %s: %s", domain, err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("bad status code: %d", resp.StatusCode)
	}

	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			cookie = c.Value
		}
	}

	if cookie == "" {
		t.Errorf("no cookie received for %s: %s", domain, err)
	}

	body, err := getBody(resp)
	if err != nil {
		t.Errorf("couldn't read response body: %s", err)
	}

	if _, ok := expected[body]; !ok {
		t.Errorf("received bad body: %s", body)
	}

	// With the Session Cookie set, we expect to always talk to the same server.
	newExpected := map[string]struct{}{
		body: {},
	}

	checkResponses(t, domain, HAProxyIPs, newExpected, cookie, client)
}

func badDomainTest(t *testing.T, domain string, HAProxyIPs []string) {
	log.Info("Bad domain test for ", domain)
	client := &http.Client{}

	for _, ip := range HAProxyIPs {
		resp, err := makeRequest(domain, ip, client, "")
		if err != nil {
			t.Errorf("request failed to %s: %s", ip, err)
		}

		if resp.StatusCode != 503 {
			t.Errorf("got unexpected response for bad domain from %s: %v",
				ip, resp)
		}
	}
}

func checkResponses(t *testing.T, domain string, HAProxyIPs []string,
	expected map[string]struct{}, cookie string, client *http.Client) {

	actualResponses := map[string]struct{}{}
	for i := 0; i < 15; i++ {
		for _, ip := range HAProxyIPs {
			resp, err := makeRequest(domain, ip, client, cookie)
			if err != nil {
				t.Errorf("request failed: %s", err)
				continue
			}

			if resp.StatusCode != 200 {
				t.Errorf("bad status code: %d", resp.StatusCode)
				continue
			}

			body, err := getBody(resp)
			if err != nil {
				t.Errorf("couldn't read response body: %s", err)
				continue
			}

			actualResponses[body] = struct{}{}
		}
	}
	assert.Equal(t, expected, actualResponses, "responses did not match expected")
}

func getHAproxyIPs(t *testing.T, machines []db.Machine,
	containers []db.Container) []string {

	minionIPMap := map[string]string{}
	for _, m := range machines {
		minionIPMap[m.PrivateIP] = m.PublicIP
	}

	var ips []string
	for _, c := range containers {
		if strings.Contains(c.Image, "haproxy") {
			ip, ok := minionIPMap[c.Minion]
			if !ok {
				t.Errorf("HAProxy with no public IP: %s", c)
				continue
			}
			ips = append(ips, ip)
		}
	}
	return ips
}

func makeRequest(domain string, HAProxyIP string, client *http.Client,
	cookie string) (*http.Response, error) {

	ip := "http://" + HAProxyIP
	req, err := http.NewRequest("GET", ip, nil)
	if err != nil {
		return nil, err
	}

	if cookie != "" {
		req.AddCookie(&http.Cookie{
			Name:  cookieName,
			Value: cookie,
		})
	}

	req.Host = domain
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func getBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
