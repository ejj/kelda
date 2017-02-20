package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/quilt/quilt/util"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/afero"
)

func TestDefaultKeys(t *testing.T) {
	util.AppFs = afero.NewMemMapFs()

	// Don't pull in keys from the host OS. Setting this environment variable
	// is safe because it won't affect the parent shell.
	os.Setenv("SSH_AUTH_SOCK", "")

	dir, err := homedir.Dir()
	if err != nil {
		t.Errorf("Failed to get homedir: %q", err.Error())
		return
	}

	sshDir := filepath.Join(dir, ".ssh")
	if err := util.AppFs.MkdirAll(sshDir, 0600); err != nil {
		t.Errorf("Failed to create SSH directory: %q", err.Error())
		return
	}

	for _, key := range []string{"id_rsa", "id_dsa", "ignored"} {
		if err := writeRandomKey(filepath.Join(sshDir, key)); err != nil {
			t.Errorf("Failed to write key: %q", err.Error())
			return
		}
	}

	signers := defaultSigners()
	if len(signers) != 2 {
		t.Errorf("Expected two default signers, but got %v", signers)
	}
}

func writeRandomKey(path string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	f, err := util.AppFs.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}
