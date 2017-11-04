package connection

import (
	"net"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// Credentials defines the credentials to use when creating a client or server.
type Credentials interface {
	// ClientOpts returns the `DialOption`s necessary to setup the credentials when
	// obtaining a grpc client connection.
	ClientOpts() []grpc.DialOption

	// ServerOpts returns the `ServerOption`s necessary to setup the credentials
	// when creating a grpc server.
	ServerOpts() []grpc.ServerOption
}

// Client creates a grpc client connected to `addr`.
func Client(proto, addr string, opts []grpc.DialOption) (*grpc.ClientConn, error) {
	timeout := 1 * time.Minute
	if proto == "unix" {
		// Unix sockets are local. Have a short timeout for quick feedback.
		timeout = 2 * time.Second
	}

	dialer := func(dialAddr string, t time.Duration) (net.Conn, error) {
		return net.DialTimeout(proto, dialAddr, t)
	}
	return grpc.Dial(addr, append(opts, grpc.WithDialer(dialer),
		grpc.WithBlock(), grpc.WithTimeout(timeout))...)
}

// Server creates a socket listening on `addr` and a grpc server. If it fails
// to open the socket, it tries again until it succeeds.
func Server(proto, addr string, opts []grpc.ServerOption) (net.Listener, *grpc.Server) {
	for {
		sock, err := net.Listen(proto, addr)
		if err == nil {
			return sock, grpc.NewServer(opts...)
		}

		log.WithError(err).Error("Failed to open socket.")
		time.Sleep(30 * time.Second)
	}
}
