//go:generate mockery -name=APIClient

package vault

import (
	"fmt"

	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/counter"

	vaultAPI "github.com/hashicorp/vault/api"
)

var vaultCounter = counter.New("Vault")

// APIClient is an interface for interacting with the Vault API. It is only
// used for unit testing -- an interface must be exported in order to generate
// a mock with mockery.
type APIClient interface {
	PutPolicy(name string, rules string) error
	DeletePolicy(name string) error
	ListPolicies() ([]string, error)
	GetPolicy(name string) (string, error)

	SetToken(token string)

	List(path string) (*vaultAPI.Secret, error)
	Read(path string) (*vaultAPI.Secret, error)
	Write(path string, data map[string]interface{}) (*vaultAPI.Secret, error)
	Delete(path string) (*vaultAPI.Secret, error)

	Init(opts *vaultAPI.InitRequest) (*vaultAPI.InitResponse, error)
	InitStatus() (bool, error)
	Unseal(shard string) (*vaultAPI.SealStatusResponse, error)
	EnableAuth(path, authType, desc string) error
}

const vaultPort = 8200

func newVaultAPIClientImpl(ip string) (APIClient, error) {
	clientConfig := &vaultAPI.Config{
		Address: fmt.Sprintf("https://%s:%d", ip, vaultPort),
	}
	err := clientConfig.ConfigureTLS(&vaultAPI.TLSConfig{
		CACert:     tlsIO.CACertPath(tlsIO.MinionTLSDir),
		ClientCert: tlsIO.SignedCertPath(tlsIO.MinionTLSDir),
		ClientKey:  tlsIO.SignedKeyPath(tlsIO.MinionTLSDir),
	})
	if err != nil {
		return nil, err
	}

	client, err := vaultAPI.NewClient(clientConfig)
	if err != nil {
		return nil, err
	}
	return vaultAPIClientImpl{client}, nil
}

type vaultAPIClientImpl struct {
	client *vaultAPI.Client
}

func (c vaultAPIClientImpl) PutPolicy(name string, rules string) error {
	vaultCounter.Inc("Put Policy")
	return c.client.Sys().PutPolicy(name, rules)
}

func (c vaultAPIClientImpl) DeletePolicy(name string) error {
	vaultCounter.Inc("Delete Policy")
	return c.client.Sys().DeletePolicy(name)
}

func (c vaultAPIClientImpl) ListPolicies() ([]string, error) {
	vaultCounter.Inc("List Policies")
	return c.client.Sys().ListPolicies()
}

func (c vaultAPIClientImpl) GetPolicy(name string) (string, error) {
	vaultCounter.Inc("Get Policy")
	return c.client.Sys().GetPolicy(name)
}

func (c vaultAPIClientImpl) SetToken(token string) {
	vaultCounter.Inc("Set Token")
	c.client.SetToken(token)
}

func (c vaultAPIClientImpl) List(path string) (*vaultAPI.Secret, error) {
	vaultCounter.Inc("List")
	return c.client.Logical().List(path)
}

func (c vaultAPIClientImpl) Read(path string) (*vaultAPI.Secret, error) {
	vaultCounter.Inc("Read")
	return c.client.Logical().Read(path)
}

func (c vaultAPIClientImpl) Write(path string, data map[string]interface{}) (
	*vaultAPI.Secret, error) {
	vaultCounter.Inc("Write")
	return c.client.Logical().Write(path, data)
}

func (c vaultAPIClientImpl) Delete(path string) (*vaultAPI.Secret, error) {
	vaultCounter.Inc("Delete")
	return c.client.Logical().Delete(path)
}

// Init initializes the Vault cluster so that the other Vault methods may be
// used. It must be called before using Vault in order to generate the
// encryption key and an initial access token. Note that if the Vault cluster
// is configured to persist data for longer than the lifetime of the Vault
// server's process, Init will not need to be called again as long as the same
// storage backend is used.
func (c vaultAPIClientImpl) Init(opts *vaultAPI.InitRequest) (*vaultAPI.InitResponse,
	error) {
	vaultCounter.Inc("Init")
	return c.client.Sys().Init(opts)
}

// InitStatus returns whether Vault has been initialized yet. See the documentation
// on Init for more information on the initialization process.
func (c vaultAPIClientImpl) InitStatus() (bool, error) {
	vaultCounter.Inc("InitStatus")
	return c.client.Sys().InitStatus()
}

// Unseal passes the Vault server the decryption key for the Vault secrets. In
// contrast to Init, it must be called once per Vault process.
func (c vaultAPIClientImpl) Unseal(shard string) (*vaultAPI.SealStatusResponse, error) {
	vaultCounter.Inc("Unseal")
	return c.client.Sys().Unseal(shard)
}

// EnableAuth enables the authentication backend at the given path. It enables
// the `authType` authentication plugin, and makes it accessible under
// `/auth/<path>`. For more information on the structure of Vault
// authentication paths, see auth.go.
func (c vaultAPIClientImpl) EnableAuth(path, authType, desc string) error {
	vaultCounter.Inc("EnableAuth")
	return c.client.Sys().EnableAuth(path, authType, desc)
}

var newVaultAPIClient = newVaultAPIClientImpl
