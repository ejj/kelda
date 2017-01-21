package etcd

import (
	"testing"
	"time"
)

func TestJoinNotifiers(t *testing.T) {
	t.Parallel()

	a := make(chan struct{})
	b := make(chan struct{})

	c := joinNotifiers(a, b)

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
