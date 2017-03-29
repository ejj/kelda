//go:generate protoc pb/pb.proto --go_out=plugins=grpc:.

package api

import (
	"fmt"
	"strings"
)

// DefaultSocket is the socket the Quilt daemon listens on by default.
var DefaultSocket = "unix:///tmp/quilt.sock"

// DefaultRemotePort is the port remote Quilt daemons (the minion) listen on by default.
const DefaultRemotePort = 9000

// ParseListenAddress validates and parses a socket address into the
// protocol and address.
func ParseListenAddress(lAddr string) (string, string, error) {
	addrParts := strings.Split(lAddr, "://")
	if len(addrParts) != 2 || (addrParts[0] != "unix" && addrParts[0] != "tcp") {
		return "", "", fmt.Errorf("malformed listen address: %s", lAddr)
	}
	return addrParts[0], addrParts[1], nil
}

// RemoteAddress creates the address string for a remote host.
func RemoteAddress(host string) string {
	return fmt.Sprintf("tcp://%s:%d", host, DefaultRemotePort)
}
