package rsa

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/kelda/kelda/connection/tls"

	"github.com/stretchr/testify/assert"
)

const (
	caCertPath     = "ca.cert"
	caKeyPath      = "ca.key"
	signedCertPath = "signed.cert"
	signedKeyPath  = "signed.key"
)

func TestGeneratedCanParse(t *testing.T) {
	ca, signed, err := newCAAndSigned()
	assert.NoError(t, err)

	_, err = tls.New(ca.CertString(), signed.CertString(), signed.PrivateKeyString())
	assert.NoError(t, err)
}

func TestGeneratedVerifies(t *testing.T) {
	ca, signed, err := newCAAndSigned()
	assert.NoError(t, err)

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ca.CertString())) {
		t.Error("Failed to parse CA")
		return
	}

	keyPair, err := New(signed.CertString(), signed.PrivateKeyString())
	assert.NoError(t, err)

	_, err = keyPair.cert.Verify(x509.VerifyOptions{
		Roots: roots,
		KeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
	})
	assert.NoError(t, err)
}

func newCAAndSigned() (KeyPair, KeyPair, error) {
	ca, err := NewCertificateAuthority()
	if err != nil {
		return KeyPair{}, KeyPair{}, err
	}

	signed, err := NewSigned(ca, pkix.Name{})
	return ca, signed, err
}
