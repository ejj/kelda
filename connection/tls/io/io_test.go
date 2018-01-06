package io

import (
	"crypto/x509/pkix"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/connection/tls/rsa"
	"github.com/kelda/kelda/util"
)

func TestWriteAndReadCA(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	// Generate a CA in memory that we will write to disk, and read back in.
	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	testDir := "/tls"

	// Write the CA.
	util.Mkdir(testDir, 0755)
	util.WriteFile(CACertPath(testDir), []byte(ca.CertString()), 0644)
	util.WriteFile(CAKeyPath(testDir), []byte(ca.PrivateKeyString()), 0600)

	parsedCA, err := ReadCA(testDir)
	assert.NoError(t, err)

	assert.Equal(t, ca.CertString(), parsedCA.CertString())
	assert.Equal(t, ca.PrivateKeyString(), parsedCA.PrivateKeyString())
}

func TestWriteAndReadMinionCerts(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	// Generate a CA and signed certificate in memory that we will write to
	// disk, and read back in.
	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	signed, err := rsa.NewSigned(ca, pkix.Name{})
	assert.NoError(t, err)

	testDir := "/tls"
	util.Mkdir(testDir, 0755)
	for _, f := range MinionFiles(testDir, ca, signed) {
		util.WriteFile(f.Path, []byte(f.Content), f.Mode)
	}

	_, err = ReadCredentials(testDir)
	assert.NoError(t, err)
}

func TestReadDaemonCerts(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	signed, err := rsa.NewSigned(ca, pkix.Name{})
	assert.NoError(t, err)

	testDir := "/tls"
	util.Mkdir(testDir, 0755)
	for _, f := range DaemonFiles(testDir, ca, signed) {
		util.WriteFile(f.Path, []byte(f.Content), f.Mode)
	}

	_, err = ReadCredentials("/tls")
	assert.NoError(t, err)

	_, err = ReadCA("/tls")
	assert.NoError(t, err)
}

func TestReadCAErrors(t *testing.T) {
	testDir := "/tls"

	// Missing certificate.
	setupFilesystem([]File{{Path: CAKeyPath(testDir), Mode: 0644}})
	_, err := ReadCA(testDir)
	assert.EqualError(t, err,
		"read cert: open /tls/certificate_authority.crt: file does not exist")

	// Missing key.
	setupFilesystem([]File{{Path: CACertPath(testDir), Mode: 0644}})
	_, err = ReadCA(testDir)
	assert.EqualError(t, err,
		"read key: open /tls/certificate_authority.key: file does not exist")
}

func TestReadCredentialsErrors(t *testing.T) {
	testDir := "/tls"

	// Missing CA certificate.
	setupFilesystem([]File{
		{Path: SignedKeyPath(testDir), Mode: 0644},
		{Path: SignedCertPath(testDir), Mode: 0644},
	})
	_, err := ReadCredentials(testDir)
	assert.EqualError(t, err,
		"read CA: open /tls/certificate_authority.crt: file does not exist")

	// Missing signed key.
	setupFilesystem([]File{
		{Path: CACertPath(testDir), Mode: 0644},
		{Path: SignedCertPath(testDir), Mode: 0644},
	})
	_, err = ReadCredentials(testDir)
	assert.EqualError(t, err,
		"read signed key: open /tls/kelda.key: file does not exist")

	// Missing signed cert.
	setupFilesystem([]File{
		{Path: CACertPath(testDir), Mode: 0644},
		{Path: SignedKeyPath(testDir), Mode: 0644},
	})
	_, err = ReadCredentials(testDir)
	assert.EqualError(t, err,
		"read signed cert: open /tls/kelda.crt: file does not exist")
}

func setupFilesystem(files []File) {
	util.AppFs = afero.NewMemMapFs()
	for _, f := range files {
		util.WriteFile(f.Path, []byte(f.Content), f.Mode)
	}
}
