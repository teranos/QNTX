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

	// Verify metadata matches what the code plugin should return
	assert.Equal(t, "code", resp.Name, "Plugin name must be 'code'")
	assert.Equal(t, "0.1.0", resp.Version, "Plugin version")
	assert.Equal(t, ">= 0.1.0", resp.QntxVersion, "Required QNTX version")
	assert.Equal(t, "Software development domain (git, GitHub, gopls, code editor)", resp.Description)

	// Regression test: Ensure we're not returning webscraper metadata via gRPC
	assert.NotEqual(t, "webscraper", resp.Name,
		"Code plugin gRPC server must not return 'webscraper' as name")
	assert.NotEqual(t, "0.2.0", resp.Version,
		"Code plugin gRPC server must not return webscraper's version")
	assert.NotContains(t, resp.Description, "Web scraping",
		"Code plugin gRPC server must not have webscraper description")

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
