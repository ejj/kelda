package util

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	var sleepCalls []time.Duration
	Sleep = func(t time.Duration) {
		sleepCalls = append(sleepCalls, t)
	}

	calls := 0
	callFiveTimes := func() bool {
		calls++
		if calls == 5 {
			return true
		}
		return false
	}
	err := BackoffWaitFor(callFiveTimes, 3*time.Second, 5*time.Second)
	assert.NoError(t, err)
	assert.Equal(t, 5, calls, "predicate should be tested 5 times")
	assert.True(t, sleepCalls[1] > sleepCalls[0], "sleep interval should increase")
	assert.Equal(t, sleepCalls[2], 3*time.Second, "sleep interval should be capped")
	assert.Equal(t, sleepCalls[3], 3*time.Second, "sleep interval should be capped")

	err = BackoffWaitFor(func() bool {
		return false
	}, 1*time.Second, 300*time.Millisecond)
	assert.EqualError(t, err, "timed out")
}

func TestJoinNotifiers(t *testing.T) {
	t.Parallel()

	a := make(chan struct{})
	b := make(chan struct{})

	c := JoinNotifiers(a, b)

	timeout := time.Tick(30 * time.Second)

	select {
	case <-c:
	case <-timeout:
		t.FailNow()
	}

	a <- struct{}{}
	select {
	case <-c:
	case <-timeout:
		t.FailNow()
	}

	b <- struct{}{}
	select {
	case <-c:
	case <-timeout:
		t.FailNow()
	}

	a <- struct{}{}
	b <- struct{}{}
	select {
	case <-c:
	case <-timeout:
		t.FailNow()
	}
}
