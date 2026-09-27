package grpc

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// declaringPlugin declares routes the way cleanAPI does, and nothing else.
type declaringPlugin struct {
	protocol.UnimplementedDomainPluginServiceServer
	routes []*protocol.RouteInfo
}

func (p *declaringPlugin) Metadata(context.Context, *protocol.Empty) (*protocol.MetadataResponse, error) {
	return &protocol.MetadataResponse{Name: "declaring", Version: "1.0.0"}, nil
}

func (p *declaringPlugin) Initialize(context.Context, *protocol.InitializeRequest) (*protocol.InitializeResponse, error) {
	return &protocol.InitializeResponse{HttpRoutes: p.routes}, nil
}

func TestExternalDomainProxy_KeepsDeclaredRoutes(t *testing.T) {
	declared := []*protocol.RouteInfo{
		{Method: "POST", Path: "/book/new", Description: "Start a booking. Takes nothing; mints an id."},
		{Method: "GET", Path: "/health", Description: "Whether the plugin loaded and reached its store."},
	}

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	protocol.RegisterDomainPluginServiceServer(server, &declaringPlugin{routes: declared})
	go server.Serve(listener)
	defer server.Stop()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(listener.Addr().String(), logger)
	require.NoError(t, err)
	defer proxy.Close()

	require.NoError(t, proxy.Initialize(context.Background(), &mockServiceRegistry{logger: logger}))

	got := proxy.GetHTTPRoutes()
	require.Len(t, got, len(declared))
	for i := range declared {
		assert.True(t, proto.Equal(declared[i], got[i]), "route %d: declared %v, kept %v", i, declared[i], got[i])
	}
}
