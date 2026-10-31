package grpc

import (
	errors "github.com/teranos/sacred-error"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// dialService connects to a service the node serves on its own loopback, which
// is why no transport credentials stand behind it. service is the name the
// error says.
func dialService(endpoint, service string) (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to %s gRPC endpoint", service)
	}
	return conn, nil
}
