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
// certificates are in place on a machine, they are left alone.
// XXX: The logic to avoid overwriting existing certificates does not work
// between restarts to the daemon. If a minion has been initialized with
// certificates but the daemon restarts, it will overwrite the existing
// certificate. However, because this code does not cause the minion to reload
// the certificate from disk, it will continue to run with the old certificates.
// This will not cause any interruption to connections as long as the same
// certificate authority is used by the daemon.
func SyncCredentials(conn db.Conn, sshKey ssh.Signer, ca rsa.KeyPair) {
	credentialedMachines := map[string]struct{}{}
	for range conn.TriggerTick(30, db.MachineTable).C {
		machines := conn.SelectFromMachine(func(m db.Machine) bool {
			return m.Status != db.Stopping && m.PublicIP != ""
		})
		syncCredentialsOnce(sshKey, ca, machines, credentialedMachines)
	}
}

func syncCredentialsOnce(sshKey ssh.Signer, ca rsa.KeyPair,
	machines []db.Machine, credentialedMachines map[string]struct{}) {
	credentialsCounter.Inc("Install to cluster")
	for _, m := range machines {
		_, hasCreds := credentialedMachines[m.PublicIP]
		if hasCreds {
			continue
		}

		credentialsCounter.Inc("Install " + m.PublicIP)
		if generateAndInstallCerts(m, sshKey, ca) {
			credentialedMachines[m.PublicIP] = struct{}{}
		}
	}
}

// generateAndInstallCerts attempts to generate a certificate key pair and install
// it onto the given machine. Returns whether it was successful.
func generateAndInstallCerts(machine db.Machine, sshKey ssh.Signer,
	ca rsa.KeyPair) bool {
	fs, err := getSftpFs(machine.PublicIP, sshKey)
	if err != nil {
		// This error is probably benign because failures to SSH are expected
		// while the machine is still booting.
		log.WithError(err).WithField("host", machine.PublicIP).
			Debug("Failed to get SFTP client. Retrying.")
		return false
	}
	defer fs.Close()

	// Generate new certificates signed by the CA for use by the minion for all
	// communication.
	signed, err := rsa.NewSigned(ca, net.ParseIP(machine.PrivateIP))
	if err != nil {
		log.WithError(err).WithField("host", machine.PublicIP).
			Error("Failed to generate certs. Retrying.")
		return false
	}

	if err := fs.MkdirAll(tlsIO.MinionTLSDir, 0755); err != nil {
		log.WithError(err).WithField("host", machine.PublicIP).Error(
			"Failed to create TLS directory. Retrying.")
		return false
	}

	for _, f := range tlsIO.MinionFiles(tlsIO.MinionTLSDir, ca, signed) {
		if err := write(fs, f.Path, f.Content, f.Mode); err != nil {
			log.WithFields(log.Fields{
				"error": err,
				"path":  f.Path,
				"host":  machine.PublicIP,
			}).Error("Failed to write file")
			return false
		}
	}

	return true
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
		User:    "quilt",
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
