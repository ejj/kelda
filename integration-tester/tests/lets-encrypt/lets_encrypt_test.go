package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"

	"github.com/stretchr/testify/assert"
)

const fakeLERootCAPem = `-----BEGIN CERTIFICATE-----
MIIFATCCAumgAwIBAgIRAKc9ZKBASymy5TLOEp57N98wDQYJKoZIhvcNAQELBQAw
GjEYMBYGA1UEAwwPRmFrZSBMRSBSb290IFgxMB4XDTE2MDMyMzIyNTM0NloXDTM2
MDMyMzIyNTM0NlowGjEYMBYGA1UEAwwPRmFrZSBMRSBSb290IFgxMIICIjANBgkq
hkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA+pYHvQw5iU3v2b3iNuYNKYgsWD6KU7aJ
diddtZQxSWYzUI3U0I1UsRPTxnhTifs/M9NW4ZlV13ZfB7APwC8oqKOIiwo7IwlP
xg0VKgyz+kT8RJfYr66PPIYP0fpTeu42LpMJ+CKo9sbpgVNDZN2z/qiXrRNX/VtG
TkPV7a44fZ5bHHVruAxvDnylpQxJobtCBWlJSsbIRGFHMc2z88eUz9NmIOWUKGGj
EmP76x8OfRHpIpuxRSCjn0+i9+hR2siIOpcMOGd+40uVJxbRRP5ZXnUFa2fF5FWd
O0u0RPI8HON0ovhrwPJY+4eWKkQzyC611oLPYGQ4EbifRsTsCxUZqyUuStGyp8oa
aoSKfF6X0+KzGgwwnrjRTUpIl19A92KR0Noo6h622OX+4sZiO/JQdkuX5w/HupK0
A0M0WSMCvU6GOhjGotmh2VTEJwHHY4+TUk0iQYRtv1crONklyZoAQPD76hCrC8Cr
IbgsZLfTMC8TWUoMbyUDgvgYkHKMoPm0VGVVuwpRKJxv7+2wXO+pivrrUl2Q9fPe
Kk055nJLMV9yPUdig8othUKrRfSxli946AEV1eEOhxddfEwBE3Lt2xn0hhiIedbb
Ftf/5kEWFZkXyUmMJK8Ra76Kus2ABueUVEcZ48hrRr1Hf1N9n59VbTUaXgeiZA50
qXf2bymE6F8CAwEAAaNCMEAwDgYDVR0PAQH/BAQDAgEGMA8GA1UdEwEB/wQFMAMB
Af8wHQYDVR0OBBYEFMEmdKSKRKDm+iAo2FwjmkWIGHngMA0GCSqGSIb3DQEBCwUA
A4ICAQBCPw74M9X/Xx04K1VAES3ypgQYH5bf9FXVDrwhRFSVckria/7dMzoF5wln
uq9NGsjkkkDg17AohcQdr8alH4LvPdxpKr3BjpvEcmbqF8xH+MbbeUEnmbSfLI8H
sefuhXF9AF/9iYvpVNC8FmJ0OhiVv13VgMQw0CRKkbtjZBf8xaEhq/YqxWVsgOjm
dm5CAQ2X0aX7502x8wYRgMnZhA5goC1zVWBVAi8yhhmlhhoDUfg17cXkmaJC5pDd
oenZ9NVhW8eDb03MFCrWNvIh89DDeCGWuWfDltDq0n3owyL0IeSn7RfpSclpxVmV
/53jkYjwIgxIG7Gsv0LKMbsf6QdBcTjhvfZyMIpBRkTe3zuHd2feKzY9lEkbRvRQ
zbh4Ps5YBnG6CKJPTbe2hfi3nhnw/MyEmF3zb0hzvLWNrR9XW3ibb2oL3424XOwc
VjrTSCLzO9Rv6s5wi03qoWvKAQQAElqTYRHhynJ3w6wuvKYF5zcZF3MDnrVGLbh1
Q9ePRFBCiXOQ6wPLoUhrrbZ8LpFUFYDXHMtYM7P9sc9IAWoONXREJaO08zgFtMp4
8iyIYUyQAbsvx8oD2M8kRvrIRSrRJSl6L957b4AFiLIQ/GgV2curs0jje7Edx34c
idWw1VrejtwclobqNMVtG3EiPUIpJGpbMcJgbiLSmKkrvQtGng==
-----END CERTIFICATE-----`

func TestLetsEncrypt(t *testing.T) {
	fmt.Println("Sleeping two minutes to give keldaio/haproxy_auto_https " +
		"time to obtain a certificate")
	time.Sleep(2 * time.Minute)

	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	// Determine the correct domain names to use based on the provider.
	provider := getProvider(t, machines)
	domainA := "ci-" + provider + "-a.kelda.io"
	domainB := "ci-" + provider + "-b.kelda.io"

	// Create a custom HTTP client that supports the Let's Encrypt Staging CA.
	certpool := x509.NewCertPool()
	certpool.AppendCertsFromPEM([]byte(fakeLERootCAPem))
	tlsConfig := &tls.Config{
		RootCAs: certpool,
	}
	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	client := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Test HTTPS requests to both domain names. The http.Client for these requests
	// has an x509 Root CertPool that only includes the Let's Encrypt Staging Root.
	// During the TLS handshake, the remote server must be using a key signed by
	// this CA, otherwise validation will fail, causing the request to error.
	expectedA := map[string]struct{}{
		"a1": {},
		"a2": {},
	}
	expectedB := map[string]struct{}{
		"b1": {},
		"b2": {},
		"b3": {},
	}
	checkResponse(t, domainA, expectedA, client)
	checkResponse(t, domainB, expectedB, client)

	checkHTTPRedirect(t, domainA, client)
	checkHTTPRedirect(t, domainB, client)
}

func getProvider(t *testing.T, machines []db.Machine) string {
	for _, m := range machines {
		if m.Role == db.Worker && m.FloatingIP != "" {
			return string(m.Provider)
		}
	}

	t.Fatalf("no worker with a floating IP")
	return ""
}

func checkHTTPRedirect(t *testing.T, domain string, client *http.Client) {
	url := "http://" + domain + "/"
	resp, err := client.Get(url)
	assert.NoError(t, err, "request failed")
	assert.Equal(t, 301, resp.StatusCode, "bad status code: expecting redirect")
}

func checkResponse(t *testing.T, domain string,
	expected map[string]struct{}, client *http.Client) {
	url := "https://" + domain + "/"

	resp, err := client.Get(url)
	assert.NoError(t, err, "request failed")
	assert.Equal(t, 200, resp.StatusCode, "bad status code")

	body, err := getBody(resp)
	assert.NoError(t, err, "couldn't read response body")

	_, ok := expected[body]
	assert.True(t, ok, "response did not match expected")
}

func getBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
