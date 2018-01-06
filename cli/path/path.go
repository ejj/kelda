package path

import (
	"os"
	"path/filepath"
)

var (
	// keldaHome is where Kelda configuration (such as TLS credentials) are
	// stored.
	// The HOME environment variable is the user's home directory on all POSIX
	// compatible systems.
	keldaHome = filepath.Join(os.Getenv("HOME"), ".kelda")

	// DefaultTLSDir is the default location for users to store TLS credentials.
	DefaultTLSDir = filepath.Join(keldaHome, "tls")

	// DefaultSSHKeyPath is the default filepath where the private SSH key used
	// to access Kelda will be stored.
	DefaultSSHKeyPath = filepath.Join(keldaHome, "ssh_key")

	// DefaultKubeSecretPath is the default location for the secret used to
	// encrypt Kubernetes resources in Etcd.
	DefaultKubeSecretPath = filepath.Join(keldaHome, "kube_etcd_secret")
)

var (
	// MinionHome is the home directory of the user on the cloud machines.
	MinionHome = "/home/kelda/.kelda"

	// MinionTLSDir is the directory in which the daemon will place TLS
	// credentials on cloud machines.
	MinionTLSDir = filepath.Join(MinionHome, "tls")

	// MinionKubeSecretPath is the filepath at which the Kubernetes encryption
	// key is located on cloud machines.
	MinionKubeSecretPath = filepath.Join(MinionHome, "kube_etcd_secret")
)
