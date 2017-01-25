package plugin

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOvsRunOnceImpl(t *testing.T) {
	var name string
	var args []string

	execRun = func(n string, a ...string) error {
		name = n
		args = a
		return nil
	}

	err := ovsRunOnceImpl([]ovsReq{{vsctlCmds: [][]string{{"a", "b"}, {"c", "d"}}}})
	assert.NoError(t, err)
	assert.Equal(t, "ovs-vsctl", name)
	assert.Equal(t, []string{"--", "a", "b", "--", "c", "d"}, args)
}

func TestOvsRun(t *testing.T) {
	var reqs []ovsReq
	ovsRunOnce = func(r []ovsReq) error {
		reqs = r
		return errors.New("err")
	}

	done := make(chan struct{})
	go func() {
		err := ovsRequestImpl([][]string{{"a"}})
		assert.EqualError(t, err, "err")
		done <- struct{}{}
	}()

	go ovsRun()

	<-done
	assert.Len(t, reqs, 1)
	assert.Equal(t, []string{"a"}, reqs[0].vsctlCmds[0])
}
