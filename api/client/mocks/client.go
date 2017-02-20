package mocks

import (
	"github.com/quilt/quilt/db"
)

// Client implements a mocked version of a Quilt client.
type Client struct {
	MachineReturn   []db.Machine
	ContainerReturn []db.Container
	EtcdReturn      []db.Etcd
	ClusterReturn   []db.Cluster
	HostReturn      string
	DeployArg       string

	MachineErr, ContainerErr, EtcdErr, ClusterErr, HostErr error
	DeployErr, ConnectionErr                               error
}

// QueryMachines retrieves the machines tracked by the Quilt daemon.
func (c *Client) QueryMachines() ([]db.Machine, error) {
	if c.MachineErr != nil {
		return nil, c.MachineErr
	}
	return c.MachineReturn, nil
}

// QueryContainers retrieves the containers tracked by the Quilt daemon.
func (c *Client) QueryContainers() ([]db.Container, error) {
	if c.ContainerErr != nil {
		return nil, c.ContainerErr
	}
	return c.ContainerReturn, nil
}

// QueryEtcd retrieves the etcd information tracked by the Quilt daemon.
func (c *Client) QueryEtcd() ([]db.Etcd, error) {
	if c.EtcdErr != nil {
		return nil, c.EtcdErr
	}
	return c.EtcdReturn, nil
}

// QueryConnections retrieves the connection information tracked by the
// Quilt daemon.
func (c *Client) QueryConnections() ([]db.Connection, error) {
	if c.ConnectionErr != nil {
		return nil, c.ConnectionErr
	}
	return nil, nil
}

// QueryLabels retrieves the label information tracked by the Quilt daemon.
func (c *Client) QueryLabels() ([]db.Label, error) {
	return nil, nil
}

// QueryClusters retrieves cluster information tracked by the Quilt daemon.
func (c *Client) QueryClusters() ([]db.Cluster, error) {
	if c.ClusterErr != nil {
		return nil, c.ClusterErr
	}
	return c.ClusterReturn, nil
}

// Close the grpc connection.
func (c *Client) Close() error {
	return nil
}

// Deploy makes a request to the Quilt daemon to deploy the given deployment.
func (c *Client) Deploy(depl string) error {
	if c.DeployErr != nil {
		return c.DeployErr
	}
	c.DeployArg = depl
	return nil
}

// Host returns the server address the Client is connected to.
func (c *Client) Host() string {
	return c.HostReturn
}
