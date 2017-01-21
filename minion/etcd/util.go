package etcd

import (
	"encoding/json"
	"sort"

	"github.com/coreos/etcd/client"
)

// joinNotifiers merges two notifications channels, `a` and `b`.  The returned channel
// will notify if one or more notifications have occurred on `a` or `b` since the last
// time it was checked.
func joinNotifiers(a, b chan struct{}) chan struct{} {
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

func writeEtcdSlice(store Store, path, old string, new sort.Interface) error {
	sort.Sort(new)
	newStr, err := jsonMarshal(new)
	if err == nil && string(newStr) != old {
		err = store.Set(path, string(newStr), 0)
	}
	return err
}

func readEtcdNode(store Store, path string) (string, error) {
	value, err := store.Get(path)
	if err != nil {
		etcdErr, ok := err.(client.Error)
		if ok && etcdErr.Code == client.ErrorCodeKeyNotFound {
			// The key was missing, which should be interpreted as empty.
			return "", nil
		}
		return "", err
	}

	return value, err
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "    ")
}
