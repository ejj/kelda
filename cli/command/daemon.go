package command

import (
	"crypto/rand"
	goRSA "crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"

	"github.com/kelda/kelda/api/server"
	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/cloud"
	"github.com/kelda/kelda/cloud/foreman"
	tlsIO "github.com/kelda/kelda/connection/tls/io"
	"github.com/kelda/kelda/connection/tls/rsa"
	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/util"
	"github.com/kelda/kelda/version"

	log "github.com/sirupsen/logrus"
)

// Daemon contains the options for running the Kelda daemon.
type Daemon struct {
	*connectionFlags
}

// NewDaemonCommand creates a new Daemon command instance.
func NewDaemonCommand() *Daemon {
	return &Daemon{
		connectionFlags: &connectionFlags{},
	}
}

var daemonCommands = "kelda daemon [OPTIONS]"
var daemonExplanation = "Start the kelda daemon, which listens for kelda API requests."

// InstallFlags sets up parsing for command line flags
func (dCmd *Daemon) InstallFlags(flags *flag.FlagSet) {
	dCmd.connectionFlags.InstallFlags(flags)
	flags.Usage = func() {
		util.PrintUsageString(daemonCommands, daemonExplanation, flags)
	}
}

// Parse parses the command line arguments for the daemon command.
func (dCmd *Daemon) Parse(args []string) error {
	return nil
}

// BeforeRun makes any necessary post-parsing transformations.
func (dCmd *Daemon) BeforeRun() error {
	return nil
}

// AfterRun performs any necessary post-run cleanup.
func (dCmd *Daemon) AfterRun() error {
	return nil
}

// Run starts the daemon.
func (dCmd *Daemon) Run() int {
	log.WithField("version", version.Version).Info("Starting Kelda daemon")

	// If the TLS credentials do not exist, autogenerate credentials and write
	// them to disk.
	if _, err := util.Stat(cliPath.DefaultTLSDir); os.IsNotExist(err) {
		log.Infof("TLS credentials not found in %s, so generating credentials "+
			"and writing to disk", cliPath.DefaultTLSDir)
		if err := setupTLS(cliPath.DefaultTLSDir); err != nil {
			log.WithError(err).WithField("path", cliPath.DefaultTLSDir).Error(
				"TLS credential generation failed")
			return 1
		}
	}

	if _, err := util.Stat(cliPath.DefaultKubeSecretPath); os.IsNotExist(err) {
		log.WithField("path", cliPath.DefaultKubeSecretPath).Info(
			"Auto-generating encryption key for Kubernetes resources")
		if err := setupKubeSecret(cliPath.DefaultKubeSecretPath); err != nil {
			log.WithError(err).Error(
				"Kubernetes encryption key generation failed")
			return 1
		}
	}

	if _, err := util.Stat(cliPath.DefaultSSHKeyPath); os.IsNotExist(err) {
		log.WithField("path", cliPath.DefaultSSHKeyPath).Info(
			"Auto-generating Kelda SSH key")
		if err := setupSSHKey(cliPath.DefaultSSHKeyPath); err != nil {
			log.WithError(err).Error("SSH key generation failed")
			return 1
		}
	}

	sshKey, err := parseSSHPrivateKey(cliPath.DefaultSSHKeyPath)
	if err != nil {
		log.WithError(err).WithField("path", cliPath.DefaultSSHKeyPath).Error(
			"Failed to parse private key")
		return 1
	}

	creds, err := tlsIO.ReadCredentials(cliPath.DefaultTLSDir)
	if err != nil {
		log.WithError(err).Error("Failed to parse TLS credentials")
		return 1
	}

	kubeSecret, err := util.ReadFile(cliPath.DefaultKubeSecretPath)
	if err != nil {
		log.WithError(err).Error("Failed to read Kubernetes encryption key")
		return 1
	}

	conn := db.New()
	go server.Run(conn, dCmd.host, true, creds)

	ca, err := tlsIO.ReadCA(cliPath.DefaultTLSDir)
	if err != nil {
		log.WithError(err).WithField("path", cliPath.DefaultTLSDir).Error(
			"Failed to parse certificate authority")
		return 1
	}

	go foreman.Run(conn, creds)
	go cloud.SyncCredentials(conn, sshKey, ca, kubeSecret)
	cloud.Run(conn, getPublicKey(sshKey))
	return 0
}

func parseSSHPrivateKey(path string) (ssh.Signer, error) {
	keyStr, err := util.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %s", err)
	}

	return ssh.ParsePrivateKey([]byte(keyStr))
}

func getPublicKey(sshPrivKey ssh.Signer) string {
	if sshPrivKey == nil {
		return ""
	}
	pubKey := base64.StdEncoding.EncodeToString(sshPrivKey.PublicKey().Marshal())
	pubKeyType := sshPrivKey.PublicKey().Type()
	return pubKeyType + " " + pubKey
}

func setupTLS(outDir string) error {
	if err := util.AppFs.MkdirAll(outDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %s", err)
	}

	ca, err := rsa.NewCertificateAuthority()
	if err != nil {
		return fmt.Errorf("failed to create CA: %s", err)
	}

	// Generate a signed certificate for use by the Daemon server, and client
	// connections.
	subject := pkix.Name{CommonName: "kelda:daemon"}
	signed, err := rsa.NewSigned(ca, subject)
	if err != nil {
		return fmt.Errorf("failed to create signed key pair: %s", err)
	}

	for _, f := range tlsIO.DaemonFiles(outDir, ca, signed) {
		if err := util.WriteFile(f.Path, []byte(f.Content), f.Mode); err != nil {
			return fmt.Errorf("failed to write file (%s): %s", f.Path, err)
		}
	}

	return nil
}

// setupSSHKey generates a new RSA key for use with SSH, and writes it to disk.
func setupSSHKey(outPath string) error {
	if err := util.AppFs.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
		return fmt.Errorf("failed to create output directory %s: %s",
			outPath, err)
	}

	key, err := goRSA.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate SSH key: %s", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	err = util.WriteFile(outPath, privateKeyPEM, 0600)
	if err != nil {
		return fmt.Errorf("failed to write to disk: %s", err)
	}
	return nil
}

func setupKubeSecret(outPath string) error {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	if err != nil {
		return err
	}
	secretBase64 := base64.StdEncoding.EncodeToString(secret)
	return util.WriteFile(outPath, []byte(secretBase64), 0400)
}
