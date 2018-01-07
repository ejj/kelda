package util

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// Sleep stores time.Sleep so we can mock it out for unit tests.
var Sleep = time.Sleep

func httpRequest(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("non-200 response status code")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(body)), err
}

// ToTar returns a tar archive named NAME and containing CONTENT.
func ToTar(name string, permissions int, content string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	hdr := &tar.Header{
		Name:    name,
		Mode:    int64(permissions),
		Size:    int64(len(content)),
		ModTime: time.Now(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}

	if _, err := tw.Write([]byte(content)); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return buf, nil
}

// MyIP gets the local systems Public IP address as visible on the WAN by querying an
// external service.
func MyIP() (string, error) {
	return httpRequest("http://checkip.amazonaws.com/")
}

// ShortUUID truncates a uuid string to 12 characters.
func ShortUUID(uuid string) string {
	if len(uuid) < 12 {
		return uuid
	}
	return uuid[:12]
}

// AppFs is an aero filesystem.  It is stored in a variable so that we can replace it
// with in-memory filesystems for unit tests.
var AppFs = afero.NewOsFs()

// Open opens a new aero file.
func Open(path string) (afero.File, error) {
	return AppFs.Open(path)
}

// WriteFile writes 'data' to the file 'filename' with the given permissions.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	a := afero.Afero{
		Fs: AppFs,
	}
	return a.WriteFile(filename, data, perm)
}

// ReadFile returns the contents of `filename`.
func ReadFile(filename string) (string, error) {
	a := afero.Afero{
		Fs: AppFs,
	}
	fileBytes, err := a.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(fileBytes), nil
}

// RemoveAll deletes the entire directory tree rooted at path.
func RemoveAll(path string) error {
	a := afero.Afero{
		Fs: AppFs,
	}
	return a.RemoveAll(path)
}

// Mkdir creates a new aero directory.
func Mkdir(path string, perm os.FileMode) error {
	a := afero.Afero{
		Fs: AppFs,
	}
	return a.Mkdir(path, perm)
}

// Stat returns file info on the given path.
func Stat(path string) (os.FileInfo, error) {
	a := afero.Afero{
		Fs: AppFs,
	}
	return a.Stat(path)
}

// FileExists checks if the given path corresponds to an existing file in the Afero
// file system.
func FileExists(path string) (bool, error) {
	a := afero.Afero{
		Fs: AppFs,
	}
	return a.Exists(path)
}

// Walk performs a traversal of the directory tree rooted at root.
func Walk(root string, walkFn filepath.WalkFunc) error {
	a := afero.Afero{
		Fs: AppFs,
	}
	return afero.Walk(a, root, walkFn)
}

// After returns whether the current time is after t. It is stored in a variable so it
// can be mocked out for unit tests.
var After = func(t time.Time) bool {
	return time.Now().After(t)
}

// BackoffWaitFor waits until `pred` is satisfied, or `timeout` Duration has
// passed. Every time the predicate fails, double the sleep interval until
// `cap` is reached.
func BackoffWaitFor(pred func() bool, cap time.Duration, timeout time.Duration) error {
	interval := 1 * time.Second
	deadline := time.Now().Add(timeout)
	for {
		if pred() {
			return nil
		}
		if After(deadline) {
			return errors.New("timed out")
		}
		Sleep(interval)

		interval *= 2
		if interval > cap {
			interval = cap
		}
	}
}

// PrintUsageString formats and prints usage strings for cli commands.
func PrintUsageString(commands string, explanation string, flags *flag.FlagSet) {
	fmt.Println("\nUsage: " + commands + "\n\n" + explanation + "\n")
	if flags != nil {
		fmt.Println("Options:")
		flags.PrintDefaults()
	}
}

// JoinNotifiers merges two notifications channels, `a` and `b`.  The returned channel
// will notify if one or more notifications have occurred on `a` or `b` since the last
// time it was checked.
func JoinNotifiers(a, b chan struct{}) chan struct{} {
	c := make(chan struct{}, 1)
	go func() {
		c <- struct{}{}
		for {
			select {
			case <-a:
			case <-b:
			}

			select {
			case c <- struct{}{}:
			default: // There's a notification in queue, no need for another.
			}
		}
	}()
	return c
}

// GetNodeBinary returns the name of the Node.js binary.
// Even though `node` is the blessed name for the Node.js binary, some package
// managers (e.g. apt-get) install the binary as `nodejs`. The `nodejs` name is
// used by package managers that already had a package named `node`, so to avoid
// accidentally executing the wrong `node` binary, we try the `nodejs` binary first.
func GetNodeBinary() (string, error) {
	if _, err := exec.LookPath("nodejs"); err == nil {
		return "nodejs", nil
	}
	if _, err := exec.LookPath("node"); err == nil {
		return "node", nil
	}
	return "", errors.New(
		"failed to locate Node.js. Is it installed and in your PATH?")
}
