package plugin

import (
	"errors"
	"testing"

	"github.com/quilt/quilt/minion/network/openflow"
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

func TestOfctlRun(t *testing.T) {
	var ofcs []openflow.Container
	addFlows = func(c []openflow.Container) error {
		ofcs = c
		return errors.New("err")
	}

	done := make(chan struct{})
	go func() {
		err := ofctlImpl(openflow.Container{Veth: "a"})
		assert.EqualError(t, err, "err")
		done <- struct{}{}
	}()

	go ofctlRun()

	<-done

	assert.Equal(t, []openflow.Container{{Veth: "a"}}, ofcs)
}
