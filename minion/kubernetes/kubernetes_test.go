//go:generate mockery -dir ../../vendor/k8s.io/client-go/kubernetes/typed/core/v1 -name ConfigMapInterface
//go:generate mockery -dir ../../vendor/k8s.io/client-go/kubernetes/typed/core/v1 -name SecretInterface
//go:generate mockery -dir ../../vendor/k8s.io/client-go/kubernetes/typed/core/v1 -name NodeInterface
//go:generate mockery -dir ../../vendor/k8s.io/client-go/kubernetes/typed/core/v1 -name PodInterface
//go:generate mockery -dir ../../vendor/k8s.io/client-go/kubernetes/typed/apps/v1 -name DeploymentInterface
//go:generate mockery -name=SecretClient
package kubernetes

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/watch"
)

func TestToStructChan(t *testing.T) {
	t.Parallel()

	timeout := time.Tick(1 * time.Second)
	watchChan := make(chan watch.Event, 2)
	structChan := toStructChan(watchChan)

	test := "Adding an event to the watchChan should trigger an event on the " +
		"structChan."
	watchChan <- watch.Event{}
	select {
	case <-structChan:
	case <-timeout:
		t.Error(test)
	}

	test = "If no event happens on the watchChan, nothing should happen on " +
		"the structChan."
	select {
	case <-structChan:
		t.Error(test)
	case <-timeout:
	}
}
