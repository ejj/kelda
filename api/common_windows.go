package api

func init() {
	// Windows doesn't support unix sockets, so we use a TCP socket instead.
	DefaultSocket = "tcp://127.0.0.1:9001"
}
