package vagrant

import (
	"errors"
	"sync"

	"github.com/quilt/quilt/cluster/acl"
	"github.com/quilt/quilt/cluster/cloudcfg"
	"github.com/quilt/quilt/cluster/machine"
	"github.com/quilt/quilt/db"
	log "github.com/Sirupsen/logrus"
	"github.com/satori/go.uuid"
)

// The Cluster object represents a connection to Amazon EC2.
type Cluster struct {
	namespace string
}

// New creates a new vagrant cluster.
func New(namespace string) (*Cluster, error) {
	clst := Cluster{namespace}
	err := addBox("boxcutter/ubuntu1604", "virtualbox")
	return &clst, err
}

// Boot creates instances in the `clst` configured according to the `bootSet`.
func (clst Cluster) Boot(bootSet []machine.Machine) error {
	// If any of the boot.Machine() calls fail, errChan will contain exactly one
	// error for this function to return.
	errChan := make(chan error, 1)

	var wg sync.WaitGroup
	for _, m := range bootSet {
		wg.Add(1)
		go func(m machine.Machine) {
			defer wg.Done()
			if err := bootMachine(m); err != nil {
				select {
				case errChan <- err:
				default:
				}
			}
		}(m)
	}
	wg.Wait()

	var err error
	select {
	case err = <-errChan:
	default:
	}

	return err
}

func bootMachine(m machine.Machine) error {
	id := uuid.NewV4().String()

	err := initMachine(cloudcfg.Ubuntu(m.SSHKeys, "xenial"), m.Size, id)
	if err == nil {
		err = up(id)
	}

	if err != nil {
		destroy(id)
	}

	return err
}

// List queries `clst` for the list of booted machines.
func (clst Cluster) List() ([]machine.Machine, error) {
	machines := []machine.Machine{}
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
		instance := machine.Machine{
			ID:        instanceID,
			PublicIP:  ip,
			PrivateIP: ip,
			Provider:  db.Vagrant,
			Size:      size(instanceID),
		}
		machines = append(machines, instance)
	}
	return machines, nil
}

// Stop shuts down `machines` in `clst.
func (clst Cluster) Stop(machines []machine.Machine) error {
	if machines == nil {
		return nil
	}
	for _, m := range machines {
		err := destroy(m.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetACLs is a noop for vagrant.
func (clst Cluster) SetACLs(acls []acl.ACL) error {
	return nil
}

// UpdateFloatingIPs is not supported.
func (clst *Cluster) UpdateFloatingIPs([]machine.Machine) error {
	return errors.New("vagrant provider does not support floating IPs")
}
