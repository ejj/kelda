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
	if len(machines) == 0 {
		return nil, errors.New("no machines to query")
	}

	// A list of errors from attempting to query the leader's IP.
	var errorStrs []string

	// Try to figure out the lead minion's IP by asking each of the machines.
	for _, m := range machines {
		if m.PublicIP == "" || m.Status == db.Stopping {
			continue
		}

		ip, err := getLeaderIP(machines, m.PublicIP, creds)
		if err == nil {
			return newClient(api.RemoteAddress(ip), creds)
		}
		errorStrs = append(errorStrs, fmt.Sprintf("%s - %s", m.PublicIP, err))
	}

	err := "no leader found"
	if len(errorStrs) != 0 {
		err += ": " + strings.Join(errorStrs, "; ")
	}

	return nil, errors.New(err)
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
