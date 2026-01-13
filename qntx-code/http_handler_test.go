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

// TestGRPCHTTPHandler tests that HTTP requests can be routed through gRPC.
// This verifies the HTTP-over-gRPC transport layer works correctly.
func TestGRPCHTTPHandler(t *testing.T) {
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

	// Initialize the plugin first (required before HTTP requests)
	initReq := &protocol.InitializeRequest{
		AtsStoreEndpoint: "localhost:0", // Not needed for this test
		QueueEndpoint:    "localhost:0", // Not needed for this test
		AuthToken:        "test-token",
		Config:           map[string]string{},
	}
	_, err = client.Initialize(context.Background(), initReq)
	require.NoError(t, err, "Initialize RPC failed")

	t.Run("GET_/api/code_returns_JSON", func(t *testing.T) {
		// Create HTTP request to get code tree
		httpReq := &protocol.HTTPRequest{
			Method: "GET",
			Path:   "/api/code",
			Headers: []*protocol.HTTPHeader{
				{Name: "Accept", Values: []string{"application/json"}},
			},
			Body: nil,
		}

		// Send HTTP request via gRPC
		httpResp, err := client.HandleHTTP(context.Background(), httpReq)
		require.NoError(t, err, "HandleHTTP RPC failed")

		// Verify response
		assert.Equal(t, int32(200), httpResp.StatusCode, "Expected 200 OK")

		// Find Content-Type header
		var contentType string
		for _, header := range httpResp.Headers {
			if header.Name == "Content-Type" {
				contentType = header.Values[0]
				break
			}
		}
		assert.Contains(t, contentType, "application/json", "Expected JSON content type")
		assert.NotEmpty(t, httpResp.Body, "Expected non-empty response body")
	})

	t.Run("GET_invalid_file_returns_400", func(t *testing.T) {
		// Create HTTP request to invalid file (not .go extension)
		httpReq := &protocol.HTTPRequest{
			Method: "GET",
			Path:   "/api/code/nonexistent",
			Headers: []*protocol.HTTPHeader{
				{Name: "Accept", Values: []string{"application/json"}},
			},
			Body: nil,
		}

		// Send HTTP request via gRPC
		httpResp, err := client.HandleHTTP(context.Background(), httpReq)
		require.NoError(t, err, "HandleHTTP RPC should not fail")

		// Verify response is 400 (handler requires .go extension)
		assert.Equal(t, int32(400), httpResp.StatusCode, "Expected 400 Bad Request for non-.go file")
	})

	t.Run("GET_nonexistent_go_file_returns_404", func(t *testing.T) {
		// Create HTTP request to nonexistent .go file
		httpReq := &protocol.HTTPRequest{
			Method: "GET",
			Path:   "/api/code/nonexistent.go",
			Headers: []*protocol.HTTPHeader{
				{Name: "Accept", Values: []string{"application/json"}},
			},
			Body: nil,
		}

		// Send HTTP request via gRPC
		httpResp, err := client.HandleHTTP(context.Background(), httpReq)
		require.NoError(t, err, "HandleHTTP RPC should not fail")

		// Verify response is 404
		assert.Equal(t, int32(404), httpResp.StatusCode, "Expected 404 Not Found for nonexistent .go file")
	})

	t.Run("POST_without_body_works", func(t *testing.T) {
		// Create HTTP POST request (git ixgest endpoint)
		httpReq := &protocol.HTTPRequest{
			Method: "POST",
			Path:   "/api/code/ixgest/git",
			Headers: []*protocol.HTTPHeader{
				{Name: "Content-Type", Values: []string{"application/json"}},
			},
			Body: []byte(`{"path": "/nonexistent", "dry_run": true}`),
		}

		// Send HTTP request via gRPC
		httpResp, err := client.HandleHTTP(context.Background(), httpReq)
		require.NoError(t, err, "HandleHTTP RPC failed")

		// Should get 400 because path doesn't exist, but the handler processed it
		assert.True(t, httpResp.StatusCode >= 400, "Expected client error for invalid path")
	})

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

// TestHealthCheck tests the plugin health check via gRPC.
func TestHealthCheck(t *testing.T) {
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

	// Call Health RPC
	resp, err := client.Health(context.Background(), &protocol.Empty{})
	require.NoError(t, err, "Health RPC failed")

	// Verify health status
	assert.True(t, resp.Healthy, "Plugin should report healthy")
	assert.NotEmpty(t, resp.Message, "Health message should not be empty")
	assert.Contains(t, resp.Details, "gopls_available", "Health details should include gopls_available")

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
