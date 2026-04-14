package grpc

import (
	"github.com/teranos/QNTX/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// dialPluginEndpoint opens a gRPC client connection to a plugin-facing service
// over the loopback interface with insecure credentials. serviceName is used
// only for the error message so failures point at the actual service.
// Extra dial options (e.g. MaxCallRecvMsgSize) are appended after the default
// insecure transport.
func dialPluginEndpoint(endpoint, serviceName string, extraOpts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append(
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
		extraOpts...,
	)
	conn, err := grpc.NewClient(endpoint, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to %s at %s", serviceName, endpoint)
	}
	return conn, nil
}
