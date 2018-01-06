package main

import (
	"crypto/x509/pkix"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/api/client"
	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/connection/tls"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/connection/tls/rsa"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

// insecureConnection defines connections with no authentication.
type insecureConnection struct{}

// ClientOpts returns the `DialOption`s necessary to setup an insecure client.
func (insecure insecureConnection) ClientOpts() []grpc.DialOption {
	return []grpc.DialOption{grpc.WithInsecure()}
}

// ServerOpts returns the `ServerOption`s necessary to setup an insecure server.
func (insecure insecureConnection) ServerOpts() []grpc.ServerOption {
	return nil
}

func TestCredentials(t *testing.T) {
	localClient, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("Failed to get local client: %s", err)
	}
	defer localClient.Close()

	machines, err := localClient.QueryMachines()
	if err != nil {
		t.Fatalf("Failed to query machines: %s", err)
	}

	// Test that an insecure connection fails.
	fmt.Println("Connecting insecurely. It should fail.")
	err = tryCredentials(machines, insecureConnection{})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(),
		"rpc error: code = Unavailable desc = transport is closing")

	// Test that connecting with the TLS directory credentials succeeds.
	fmt.Println("Connecting with the daemon's TLS directory. It should succeed.")
	tlsCreds, err := tlsIO.ReadCredentials(cliPath.DefaultTLSDir)
	assert.NoError(t, err)
	err = tryCredentials(machines, tlsCreds)
	assert.NoError(t, err)

	// Test that connecting with random TLS credentials fails.
	fmt.Println("Connecting with the random TLS credentials. It should fail.")
	tlsCreds, err = randomTLSCredentials()
	assert.NoError(t, err)
	err = tryCredentials(machines, tlsCreds)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "x509: certificate signed by unknown authority")
}

// Try querying the containers from each of the machines using the given credentials.
func tryCredentials(machines []db.Machine, creds connection.Credentials) error {
	for _, m := range machines {
		c, err := client.New(api.RemoteAddress(m.PublicIP), creds)
		if err != nil {
			return err
		}
		defer c.Close()

		if _, err = c.QueryContainers(); err != nil {
			return err
		}
	}
	return nil
}

// Generate random TLS credentials.
func randomTLSCredentials() (tls.TLS, error) {
	ca, err := rsa.NewCertificateAuthority()
	if err != nil {
		return tls.TLS{}, err
	}

	signed, err := rsa.NewSigned(ca, pkix.Name{CommonName: "ci"})
	if err != nil {
		return tls.TLS{}, err
	}

	return tls.New(ca.CertString(), signed.CertString(), signed.PrivateKeyString())
}
