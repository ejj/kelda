package cloud

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/spf13/afero/sftpfs"
	"golang.org/x/crypto/ssh"

	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/connection/tls/rsa"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
)

var credentialsCounter = counter.New("Cloud Credentials")

// SyncCredentials installs TLS certificates on all machines. It generates
// the certificates using the given certificate authority, and copies them
// over using the given ssh key. It only installs certificates once -- once
// certificates are in place on a machine, they are left alone. SyncCredentials
// also writes the installed signed certificate for each machine into the
// database.
func SyncCredentials(conn db.Conn, sshKey ssh.Signer, ca rsa.KeyPair) {
	for range conn.TriggerTick(30, db.MachineTable).C {
		syncCredentialsOnce(conn, sshKey, ca)
	}
}

func syncCredentialsOnce(conn db.Conn, sshKey ssh.Signer, ca rsa.KeyPair) {
	credentialsCounter.Inc("Install to cluster")

	// Only attempt to install certificates on machines that are running, and
	// that do not already have certificates.
	machines := conn.SelectFromMachine(func(m db.Machine) bool {
		return m.Status != db.Stopping && m.PublicIP != "" && m.PublicKey == ""
	})

	ipCertMap := map[string]string{}
	for _, m := range machines {
		credentialsCounter.Inc("Install " + m.PublicIP)
		publicKey, ok := generateAndInstallCerts(m, sshKey, ca)
		if ok {
			ipCertMap[m.PublicIP] = publicKey
		}
	}

	conn.Txn(db.MachineTable).Run(func(view db.Database) error {
		for _, dbm := range view.SelectFromMachine(nil) {
			if cert, ok := ipCertMap[dbm.PublicIP]; ok {
				dbm.PublicKey = cert
				view.Commit(dbm)
			}
		}
		return nil
	})
}

// generateAndInstallCerts attempts to generate a certificate key pair and install
// it onto the given machine. If a certificate was already installed, it simply
// returns the contents of the previously installed certificate. Returns the
// public key of the installed certificate, and whether it was successful.
func generateAndInstallCerts(machine db.Machine, sshKey ssh.Signer,
	ca rsa.KeyPair) (string, bool) {
	fs, err := getSftpFs(machine.PublicIP, sshKey)
	if err != nil {
		// This error is probably benign because failures to SSH are expected
		// while the machine is still booting.
		log.WithError(err).WithField("host", machine.PublicIP).
			Debug("Failed to get SFTP client. Retrying.")
		return "", false
	}
	defer fs.Close()

	certPath := tlsIO.SignedCertPath(tlsIO.MinionTLSDir)
	if _, err := fs.Stat(certPath); err == nil {
		existingCert, err := afero.Afero{Fs: fs}.ReadFile(certPath)
		if err != nil {
			log.WithError(err).WithField("host", machine.PublicIP).Error(
				"Failed to read existing certificate")
			return "", false
		}
		return string(existingCert), true
	}

	// Generate new certificates signed by the CA for use by the minion for all
	// communication.
	signed, err := rsa.NewSigned(ca, net.ParseIP(machine.PrivateIP))
	if err != nil {
		log.WithError(err).WithField("host", machine.PublicIP).
			Error("Failed to generate certs. Retrying.")
		return "", false
	}

	// Create the directory in which the credentials will be installed. This is
	// usually a no-op because the cloud config (cloud/cfg/template.go) creates
	// the directory at boot to prevent a race condition with Docker.
	if err := fs.MkdirAll(tlsIO.MinionTLSDir, 0755); err != nil {
		log.WithError(err).WithField("host", machine.PublicIP).Error(
			"Failed to create TLS directory. Retrying.")
		return "", false
	}

	for _, f := range tlsIO.MinionFiles(tlsIO.MinionTLSDir, ca, signed) {
		if err := write(fs, f.Path, f.Content, f.Mode); err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"path":  f.Path,
				"host":  machine.PublicIP,
			}).Error("Failed to write file")
			return "", false
		}
	}

	return signed.CertString(), true
}

func write(fs afero.Fs, path, contents string, mode os.FileMode) error {
	f, err := fs.Create(path)
	if err != nil {
		return fmt.Errorf("create: %s", err)
	}
	defer f.Close()

	if err := fs.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod: %s", err)
	}

	if _, err := io.WriteString(f, contents); err != nil {
		return fmt.Errorf("write: %s", err)
	}

	return nil
}

// sftpFs is a wrapper that allows closing the sftp connection.
type sftpFs interface {
	afero.Fs
	Close() error
}

type sftpFsImpl struct {
	afero.Fs

	client *sftp.Client
}

func (fs sftpFsImpl) Close() error {
	return fs.client.Close()
}

// getSftpFsImpl gets an SFTP connection to `host` authenticated by `sshKey`.
func getSftpFsImpl(host string, sshKey ssh.Signer) (sftpFs, error) {
	sshConfig := &ssh.ClientConfig{
		User:    "kelda",
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(sshKey)},
		Timeout: 5 * time.Second,
		// XXX: We have to ignore the host key because we don't keep track of
		// the host keys of machines. Once we do, this should use strict host
		// key checking. For now, this means that a machine could theoretically
		// man in the middle as the target machine and obtain signed
		// certificates.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", host), sshConfig)
	if err != nil {
		return nil, fmt.Errorf("dial: %s", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("sftp: %s", err)
	}

	return sftpFsImpl{sftpfs.New(sftpClient), sftpClient}, nil
}

// Saved in a variable to allow injecting a memory filesystem during unit testing.
var getSftpFs = getSftpFsImpl
