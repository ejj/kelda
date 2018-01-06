package cloud

import (
	"crypto/rand"
	goRSA "crypto/rsa"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"

	cliPath "github.com/kelda/kelda/cli/path"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/connection/tls/rsa"
	"github.com/kelda/kelda/db"
)

// Test the success path when generating and installing TLS credentials on a
// new machine.
func TestSyncCredentialsTLS(t *testing.T) {
	key, err := goRSA.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	expSigner, err := ssh.NewSignerFromKey(key)
	assert.NoError(t, err)

	expHost := "8.8.8.8"
	mockFs := afero.NewMemMapFs()
	conn := db.New()

	getSftpFs = func(host string, signer ssh.Signer) (sftpFs, error) {
		assert.Equal(t, expSigner, signer)
		assert.Equal(t, expHost, host)
		return mockSFTPFs{mockFs}, nil
	}

	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		dbm := view.InsertMachine()
		dbm.PublicIP = expHost
		dbm.PrivateIP = "9.9.9.9"
		view.Commit(dbm)
		return nil
	})
	syncCredentialsOnce(conn, expSigner, ca, "")

	aferoFs := afero.Afero{Fs: mockFs}
	certBytes, err := aferoFs.ReadFile(tlsIO.SignedCertPath(cliPath.MinionTLSDir))
	assert.NoError(t, err)
	assert.NotEmpty(t, certBytes)

	keyBytes, err := aferoFs.ReadFile(tlsIO.SignedKeyPath(cliPath.MinionTLSDir))
	assert.NoError(t, err)
	assert.NotEmpty(t, keyBytes)

	caBytes, err := aferoFs.ReadFile(tlsIO.CACertPath(cliPath.MinionTLSDir))
	assert.NoError(t, err)
	assert.NotEmpty(t, caBytes)
}

func TestExistingTLSCredentialsDontGetOverwritten(t *testing.T) {
	conn := db.New()
	mockFs := afero.NewMemMapFs()
	aferoFs := afero.Afero{Fs: mockFs}
	getSftpFs = func(_ string, _ ssh.Signer) (sftpFs, error) {
		return mockSFTPFs{mockFs}, nil
	}

	existingCert := "existingCert"
	err := aferoFs.WriteFile(
		tlsIO.SignedCertPath(cliPath.MinionTLSDir),
		[]byte(existingCert),
		0644)
	assert.NoError(t, err)

	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		dbm := view.InsertMachine()
		dbm.PublicIP = "foo"
		dbm.PrivateIP = "bar"
		view.Commit(dbm)
		return nil
	})
	syncCredentialsOnce(conn, nil, ca, "")

	// Ensure that the existing public key was not overwritten.
	certOnMachine, err := aferoFs.ReadFile(
		tlsIO.SignedCertPath(cliPath.MinionTLSDir))
	assert.NoError(t, err)
	assert.Equal(t, existingCert, string(certOnMachine))
}

func TestSyncKubeSecret(t *testing.T) {
	machineCloudID := "cloudID"
	machinePubIP := "pubIP"
	kubeSecret := "kubeSecret"
	machinesWithTLS = map[string]struct{}{
		machineCloudID: {},
	}
	machinesWithKubeSecret = map[string]struct{}{}

	conn := db.New()
	mockFs := afero.NewMemMapFs()
	aferoFs := afero.Afero{Fs: mockFs}
	getSftpFs = func(_ string, _ ssh.Signer) (sftpFs, error) {
		return mockSFTPFs{mockFs}, nil
	}

	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		dbm := view.InsertMachine()
		dbm.PublicIP = machinePubIP
		dbm.CloudID = machineCloudID
		dbm.Role = db.Master
		view.Commit(dbm)
		return nil
	})
	syncCredentialsOnce(conn, nil, rsa.KeyPair{}, kubeSecret)

	// Test writing the secret.
	kubeSecretOnMachine, err := aferoFs.ReadFile(cliPath.MinionKubeSecretPath)
	assert.NoError(t, err)
	assert.Equal(t, kubeSecret, string(kubeSecretOnMachine))

	// Simulate the daemon restarting by clearing the `machinesWithKubeSecret`
	// map. We should recognize that the minion already has the secret, and not
	// overwrite it.
	machinesWithKubeSecret = map[string]struct{}{}
	syncCredentialsOnce(conn, nil, rsa.KeyPair{}, "ignoreme")
	kubeSecretOnMachine, err = aferoFs.ReadFile(cliPath.MinionKubeSecretPath)
	assert.NoError(t, err)
	assert.Equal(t, kubeSecret, string(kubeSecretOnMachine))

	// Test that when syncCredentialsOnce is called again, it doesn't attempt
	// to reconfigure the secret since it successfully configured the machine
	// in the above test.
	getSftpFs = func(_ string, _ ssh.Signer) (sftpFs, error) {
		assert.FailNow(t, "a SFTP client should not be created")
		return nil, errors.New("unreached")
	}
	syncCredentialsOnce(conn, nil, rsa.KeyPair{}, kubeSecret)
}

func TestFailedToSSH(t *testing.T) {
	ca, err := rsa.NewCertificateAuthority()
	assert.NoError(t, err)

	conn := db.New()
	cloudID := "cloudID"
	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		dbm := view.InsertMachine()
		dbm.PublicIP = "8.8.8.8"
		dbm.PrivateIP = "9.9.9.9"
		dbm.CloudID = cloudID
		view.Commit(dbm)
		return nil
	})

	getSftpFs = func(host string, _ ssh.Signer) (sftpFs, error) {
		return nil, assert.AnError
	}

	syncCredentialsOnce(conn, nil, ca, "")
	assert.NotContains(t, cloudID, machinesWithTLS)
}

type mockSFTPFs struct {
	afero.Fs
}

func (fs mockSFTPFs) Close() error {
	return nil
}
