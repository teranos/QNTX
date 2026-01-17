package qntxcode

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestGRPCMetadata tests that the plugin returns correct metadata via gRPC.
// This is an integration test that starts the actual gRPC server.
func TestGRPCMetadata(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	// Create the code plugin
	plugin := NewPlugin()

	// Create gRPC server wrapper
	server := plugingrpc.NewPluginServer(plugin, logger)

	// Find available port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "Failed to allocate port")
	addr := listener.Addr().String()
	listener.Close()

	// Start server in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(ctx, addr)
	}()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Connect as a client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "Failed to create gRPC client")
	defer conn.Close()

	client := protocol.NewDomainPluginServiceClient(conn)

	// Call Metadata RPC
	resp, err := client.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err, "Metadata RPC failed")

	// Verify metadata is returned via gRPC
	assert.Equal(t, "code", resp.Name, "Plugin name must be 'code'")
	assert.NotEmpty(t, resp.Version, "Version must not be empty")
	assert.NotEmpty(t, resp.QntxVersion, "Required QNTX version must not be empty")
	assert.NotEmpty(t, resp.Description, "Description must not be empty")

	// Shutdown
	cancel()
	select {
	case err := <-serverErr:
		if err != nil {
			t.Logf("Server shutdown with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Server did not shutdown within timeout")
	}
}
