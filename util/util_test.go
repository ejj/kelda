package util

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestToTar(t *testing.T) {
	content := fmt.Sprintf("a b c\neasy as\n1 2 3")
	out, err := ToTar("test_tar", 0644, content)

	if err != nil {
		t.Errorf("Error %#v while writing tar archive, expected nil", err.Error())
	}

	var buffOut bytes.Buffer
	writer := io.Writer(&buffOut)

	for tr := tar.NewReader(out); err != io.EOF; _, err = tr.Next() {
		if err != nil {
			t.Errorf("Error %#v while reading tar archive, expected nil",
				err.Error())
		}

		_, err = io.Copy(writer, tr)
		if err != nil {
			t.Errorf("Error %#v while reading tar archive, expected nil",
				err.Error())
		}
	}

	actual := buffOut.String()
	if actual != content {
		t.Error("Generated incorrect tar archive.")
	}
}

func TestWaitFor(t *testing.T) {
	Sleep = func(t time.Duration) {}

	calls := 0
	callThreeTimes := func() bool {
		calls++
		if calls == 3 {
			return true
		}
		return false
	}
	err := WaitFor(callThreeTimes, 1*time.Second, 5*time.Second)
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
	}
	if calls != 3 {
		t.Errorf("Incorrect number of calls to predicate: %d", calls)
	}

	err = WaitFor(func() bool {
		return false
	}, 1*time.Second, 300*time.Millisecond)
	if err.Error() != "timed out" {
		t.Errorf("Expected waitFor to timeout")
	}
}
