package getter

import (
	"errors"
	"fmt"

	log "github.com/Sirupsen/logrus"

	"github.com/quilt/quilt/api"
	"github.com/quilt/quilt/api/client"
	"github.com/quilt/quilt/api/util"
)

// New returns an implementation of the Getter interface.
func New() client.Getter {
	return clientGetterImpl{addrClientGetterImpl{}}
}

// We create a separate interface for getting a client given an address so that
// ContainerClient and LeaderClient can be unit tested.
type addrClientGetter interface {
	Client(string) (client.Client, error)
}

type addrClientGetterImpl struct{}

type clientGetterImpl struct {
	addrClientGetter
}

func (getter addrClientGetterImpl) Client(host string) (client.Client, error) {
	c, err := client.New(host)
	if err != nil {
		return nil, daemonConnectError{
			host:         host,
			connectError: err,
		}
	}
	return c, nil
}

func (getter clientGetterImpl) LeaderClient(localClient client.Client) (
	client.Client, error) {

	machines, err := localClient.QueryMachines()
	if err != nil {
		return nil, fmt.Errorf("unable to query machines: %s", err.Error())
	}

	// Try to figure out the lead minion's IP by asking each of the machines
	// tracked by the local daemon.
	for _, m := range machines {
		if m.PublicIP == "" {
			continue
		}

		ip, err := getter.getLeaderIP(localClient, m.PublicIP)
		if err == nil {
			return getter.Client(api.RemoteAddress(ip))
		}
		log.WithError(err).Debug("Unable to get leader IP")
	}

	return nil, errors.New("no leader found")
}

func (getter clientGetterImpl) ContainerClient(localClient client.Client,
	stitchID string) (client.Client, error) {

	leaderClient, err := getter.LeaderClient(localClient)
	if err != nil {
		return nil, err
	}
	defer leaderClient.Close()

	containerInfo, err := util.GetContainer(leaderClient, stitchID)
	if err != nil {
		return nil, err
	}

	if containerInfo.Minion == "" {
		return nil, errors.New("container hasn't been scheduled yet")
	}

	containerIP, err := getPublicIP(localClient, containerInfo.Minion)
	if err != nil {
		return nil, err
	}

	return getter.Client(api.RemoteAddress(containerIP))
}

// Get the public IP of the lead minion by querying the remote machine's etcd
// table for the private IP, and then searching for the public IP in the local
// daemon.
func (getter clientGetterImpl) getLeaderIP(localClient client.Client,
	daemonIP string) (string, error) {

	remoteClient, err := getter.Client(api.RemoteAddress(daemonIP))
	if err != nil {
		return "", err
	}
	defer remoteClient.Close()

	etcds, err := remoteClient.QueryEtcd()
	if err != nil {
		return "", err
	}

	if len(etcds) == 0 || etcds[0].LeaderIP == "" {
		return "", fmt.Errorf("no leader information on host %s", daemonIP)
	}

	return getPublicIP(localClient, etcds[0].LeaderIP)
}

// getPublicIP returns the public IP associated with the machine with the
// given private IP.
func getPublicIP(c client.Client, privateIP string) (string, error) {
	machines, err := c.QueryMachines()
	if err != nil {
		return "", err
	}

	for _, m := range machines {
		if m.PrivateIP == privateIP {
			return m.PublicIP, nil
		}
	}

	return "", fmt.Errorf("no machine with private IP %s", privateIP)
}

// daemonConnectError represents when we are unable to connect to the Quilt daemon.
type daemonConnectError struct {
	host         string
	connectError error
}

func (err daemonConnectError) Error() string {
	return fmt.Sprintf("Unable to connect to the Quilt daemon at %s: %s. "+
		"Is the quilt daemon running? If not, you can start it with "+
		"`quilt daemon`.", err.host, err.connectError.Error())
}
