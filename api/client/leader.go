package client

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kelda/kelda/api"
	"github.com/kelda/kelda/connection"
	"github.com/kelda/kelda/db"
)

// Leader obtains a Client connected to the Leader of the cluster.
func Leader(machines []db.Machine, creds connection.Credentials) (Client, error) {
	leaderIP, err := GetLeaderIP(machines, creds)
	if err != nil {
		return nil, err
	}
	return newClient(api.RemoteAddress(leaderIP), creds)
}

// GetLeaderIP queries the given machines for the IP of the leader of cluster.
func GetLeaderIP(machines []db.Machine, creds connection.Credentials) (string, error) {
	if len(machines) == 0 {
		return "", errors.New("no machines to query")
	}

	// A list of errors from attempting to query the leader's IP.
	var errorStrs []string

	// Try to figure out the lead minion's IP by asking each of the machines.
	for _, m := range machines {
		if m.PublicIP == "" || m.Status != db.Connected {
			continue
		}

		ip, err := getLeaderIP(machines, m.PublicIP, creds)
		if err == nil {
			return ip, nil
		}
		errorStrs = append(errorStrs, fmt.Sprintf("%s - %s", m.PublicIP, err))
	}

	return "", NoLeaderError(errorStrs)
}

// NoLeaderError wraps the errors resulting from trying to find the leader of
// the cluster. It is exported so that callers can tell whether a call to `Leader`
// failed because it was unable to find the leader.
type NoLeaderError []string

func (errorStrs NoLeaderError) Error() string {
	err := "no leader found"
	if len(errorStrs) != 0 {
		err += ": " + strings.Join(errorStrs, "; ")
	}
	return err
}

// Get the public IP of the lead minion by querying the remote machine's etcd
// table for the private IP, and then searching for the public IP in the local
// daemon.
func getLeaderIP(machines []db.Machine, daemonIP string, creds connection.Credentials) (
	string, error) {
	remoteClient, err := newClient(api.RemoteAddress(daemonIP), creds)
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

	ip := etcds[0].LeaderIP
	for _, m := range machines {
		if m.PrivateIP == ip {
			return m.PublicIP, nil
		}
	}

	return "", fmt.Errorf("no machine with private IP %s", ip)
}

// New is saved in a variable to facilitate injecting test clients for
// unit testing.
var newClient = New
