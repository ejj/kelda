package vagrant

import (
	"errors"
	"sync"

	"github.com/kelda/kelda/cloud/acl"
	"github.com/kelda/kelda/cloud/cfg"
	"github.com/kelda/kelda/counter"
	"github.com/kelda/kelda/db"
	"github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
)

// The Provider object represents a connection to Vagrant.
type Provider struct {
	namespace string
}

var c = counter.New("Vagrant")

// New creates a new vagrant provider.
func New(namespace string) (*Provider, error) {
	prvdr := Provider{namespace}
	err := addBox(box, "virtualbox")
	return &prvdr, err
}

// Boot creates instances in the `prvdr` configured according to the `bootSet`.
func (prvdr Provider) Boot(bootSet []db.Machine) ([]string, error) {
	for _, m := range bootSet {
		if m.Preemptible {
			return nil, errors.New(
				"vagrant does not support preemptible instances")
		}
	}

	// If any of the boot.Machine() calls fail, errChan will contain exactly one
	// error for this function to return.
	errChan := make(chan error, 1)

	var ids []string
	var wg sync.WaitGroup
	for _, m := range bootSet {
		wg.Add(1)
		id := uuid.NewV4().String()
		go func(id string, m db.Machine) {
			defer wg.Done()
			if err := bootMachine(id, m); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(id, m)
		ids = append(ids, id)
	}
	wg.Wait()

	var err error
	select {
	case err = <-errChan:
	default:
	}

	return ids, err
}

func bootMachine(id string, m db.Machine) error {
	err := initMachine(cfg.Ubuntu(m, inboundPublicInterface), m.Size, id)
	if err == nil {
		err = up(id)
	}

	if err != nil {
		destroy(id)
	}

	return err
}

// List queries `prvdr` for the list of booted machines.
func (prvdr Provider) List() ([]db.Machine, error) {
	machines := []db.Machine{}
	instanceIDs, err := list()

	if err != nil {
		return machines, err
	} else if len(instanceIDs) == 0 {
		return machines, nil
	}

	for _, instanceID := range instanceIDs {
		ip, err := publicIP(instanceID)
		if err != nil {
			log.WithError(err).Infof(
				"Failed to retrieve IP address for %s.",
				instanceID)
		}
		instance := db.Machine{
			Provider:  db.Vagrant,
			CloudID:   instanceID,
			PublicIP:  ip,
			PrivateIP: ip,
			Size:      size(instanceID),
		}
		machines = append(machines, instance)
	}
	return machines, nil
}

// Stop shuts down `machines` in `prvdr.
func (prvdr Provider) Stop(machines []db.Machine) error {
	if machines == nil {
		return nil
	}
	for _, m := range machines {
		err := destroy(m.CloudID)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetACLs is a noop for vagrant.
func (prvdr Provider) SetACLs(acls []acl.ACL) error {
	return nil
}

// UpdateFloatingIPs is not supported.
func (prvdr *Provider) UpdateFloatingIPs([]db.Machine) error {
	return errors.New("vagrant provider does not support floating IPs")
}
