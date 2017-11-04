package ssh

import (
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"

	cliPath "github.com/kelda/kelda/cli/path"
	"github.com/kelda/kelda/util"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"
)

// NativeClient is wrapper over Go's SSH client.
type NativeClient struct {
	*ssh.Client
}

// New returns an SSH Client connected to the given host.
func New(host string, keyPath string) (Client, error) {
	var auth ssh.AuthMethod
	if keyPath != "" {
		signer, err := signerFromFile(keyPath)
		if err != nil {
			return NativeClient{}, err
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.PublicKeys(defaultSigners()...)
	}
	sshConfig := &ssh.ClientConfig{
		User:            "kelda",
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", host), sshConfig)
	return NativeClient{client}, err
}

// Gets the signers for the default private key locations if possible
func defaultSigners() []ssh.Signer {
	var signers []ssh.Signer
	if sa, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentSigners, err := agent.NewClient(sa).Signers()
		if err != nil {
			log.WithError(err).Warning("Error getting keys from ssh-agent")
		} else {
			signers = agentSigners
		}
	}

	dir, err := homedir.Dir()
	if err != nil {
		log.WithError(err).Warn("Error getting path of home directory")
		return signers
	}

	pathsToTry := []string{cliPath.DefaultSSHKeyPath}
	for _, keyName := range []string{"id_rsa", "id_dsa", "id_ecdsa",
		"id_ed25519", "quilt", "kelda"} {
		pathsToTry = append(pathsToTry, filepath.Join(dir, ".ssh", keyName))
	}

	for _, identityPath := range pathsToTry {
		key, err := signerFromFile(identityPath)
		if err != nil {
			if os.IsNotExist(err) {
				log.WithField("path", identityPath).
					Debug("Key does not exist")
			} else {
				log.WithError(err).WithField("path", identityPath).
					Warn("Unable to load default identity file")
			}
			continue
		}
		signers = append(signers, key)
	}
	return signers
}

// Run runs an SSH command.
func (c NativeClient) Run(requestPTY bool, command string) error {
	session, err := c.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if requestPTY {
		session.Stdin = os.Stdin

		pty := newPty(session)
		if err := pty.Request(); err != nil {
			return err
		}
		defer pty.Close()
	}

	return session.Run(command)
}

// CombinedOutput runs an SSH command, returning the combined stdin and stdout.
func (c NativeClient) CombinedOutput(command string) ([]byte, error) {
	session, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	return session.CombinedOutput(command)
}

// Shell starts a login shell.
func (c NativeClient) Shell() error {
	s, err := c.NewSession()
	if err != nil {
		return err
	}
	defer s.Close()

	s.Stdin = os.Stdin
	s.Stdout = os.Stdout
	s.Stderr = os.Stderr
	pty := newPty(s)
	if err := pty.Request(); err != nil {
		return err
	}
	defer pty.Close()

	if err := s.Shell(); err != nil {
		return err
	}
	return s.Wait()
}

// pty encapsulates pseudo-terminal operations.
type pty struct {
	session        *ssh.Session
	fileDescriptor int
	originalState  *terminal.State
	resizeSignal   chan os.Signal
	height         int
	width          int
}

func newPty(session *ssh.Session) *pty {
	return &pty{
		session:      session,
		resizeSignal: make(chan os.Signal, 1),
	}
}

// Request requests a PTY with opinionated defaults.
func (p *pty) Request() error {
	p.fileDescriptor = int(os.Stdin.Fd())
	if !terminal.IsTerminal(p.fileDescriptor) {
		return errors.New("TTY should be requested from a terminal")
	}

	var err error
	p.originalState, err = terminal.MakeRaw(p.fileDescriptor)
	if err != nil {
		return err
	}

	p.width, p.height, err = terminal.GetSize(p.fileDescriptor)
	if err != nil {
		return err
	}

	err = p.session.RequestPty("xterm", p.height, p.width, ssh.TerminalModes{
		ssh.ECHO:          1,     // display what's being typed
		ssh.ECHOCTL:       1,     // allow ^CONTROL_CHARACTERS
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	})
	if err != nil {
		return err
	}

	setupResizeSignal(p.resizeSignal)
	go p.monitorWindowSize()
	return nil
}

// Close tears down the TTY, restoring the terminal to its original state.
func (p *pty) Close() error {
	signal.Stop(p.resizeSignal)
	return terminal.Restore(p.fileDescriptor, p.originalState)
}

// windowChange is the payload for the request that tells the remote SSH server
// to adjust the dimensions of future message bodies.
// Source: https://www.ietf.org/rfc/rfc4254.txt 6.7: Window Dimension Change Message
type windowChange struct {
	columns  uint32
	rows     uint32
	widthPx  uint32
	heightPx uint32
}

func (p *pty) monitorWindowSize() {
	for range p.resizeSignal {
		width, height, err := terminal.GetSize(p.fileDescriptor)
		if err != nil {
			log.WithError(err).Warn("Error getting terminal window size")
			continue
		}

		if p.width == width && p.height == height {
			continue
		}

		p.width = width
		p.height = height

		payload := ssh.Marshal(windowChange{
			columns:  uint32(width),
			rows:     uint32(height),
			widthPx:  0,
			heightPx: 0,
		})
		_, err = p.session.SendRequest("window-change", false, payload)
		if err != nil {
			log.WithError(err).Warn("Error adjusting terminal window size")
		}
	}
}

func signerFromFile(file string) (ssh.Signer, error) {
	fileStr, err := util.ReadFile(file)
	if err != nil {
		return nil, err
	}

	// Parse the key to give a helpul error when it's encrypted.
	keyBytes := []byte(fileStr)
	block, _ := pem.Decode(keyBytes)
	if block != nil && block.Headers["Proc-Type"] == "4,ENCRYPTED" {
		return nil, errors.New("ssh: password protected keys are " +
			"not supported, try adding the key to ssh-agent first using " +
			"`ssh-add`")
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}
