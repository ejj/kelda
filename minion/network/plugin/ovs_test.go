package plugin

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVsctlRun(t *testing.T) {
	var name string
	var args []string
	execRun = func(n string, a ...string) error {
		name = n
		args = a
		return errors.New("err")
	}

	done := make(chan struct{})
	go func() {
		err := vsctlImpl([][]string{{"a"}, {"b"}})
		assert.EqualError(t, err, "err")
		done <- struct{}{}
	}()

	go vsctlRun()

	<-done

	assert.Equal(t, "ovs-vsctl", name)
	assert.Equal(t, []string{"--", "a", "--", "b"}, args)
}
