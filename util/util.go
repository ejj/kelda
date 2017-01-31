package util

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// Sleep stores time.Sleep so we can mock it out for unit tests.
var Sleep = time.Sleep

func httpRequest(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "<error>", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "<error>", err
	}

	return strings.TrimSpace(string(body)), err
}

// ToTar returns a tar archive named NAME and containing CONTENT.
func ToTar(name string, permissions int, content string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	hdr := &tar.Header{
		Name: name,
		Mode: int64(permissions),
		Size: int64(len(content)),
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
// exeternal service.
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

// StrSliceEqual returns true of the string slices 'x' and 'y' are identical.
func StrSliceEqual(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if v != y[i] {
			return false
		}
	}
	return true
}

// StrStrMapEqual returns true of the string->string maps 'x' and 'y' are equal.
func StrStrMapEqual(x, y map[string]string) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if yVal, ok := y[k]; !ok {
			return false
		} else if v != yVal {
			return false
		}
	}
	return true
}

// After returns whether the current time is after t. It is stored in a variable so it
// can be mocked out for unit tests.
var After = func(t time.Time) bool {
	return time.Now().After(t)
}

// WaitFor waits until `pred` is satisfied, or `timeout` Duration has passed, checking
// at every `interval`.
func WaitFor(pred func() bool, interval time.Duration, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if pred() {
			return nil
		}
		if After(deadline) {
			return errors.New("timed out")
		}
		Sleep(interval)
	}
}
