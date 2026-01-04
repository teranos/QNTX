package grpc

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/domains"
	"github.com/teranos/QNTX/domains/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// mockPlugin is a minimal plugin for testing
type mockPlugin struct {
	initCalled     bool
	shutdownCalled bool
}

func (p *mockPlugin) Metadata() domains.Metadata {
	return domains.Metadata{
		Name:        "mock",
		Version:     "1.0.0",
		QNTXVersion: ">= 0.1.0",
		Description: "Mock plugin for testing",
		Author:      "Test",
		License:     "MIT",
	}
}

func (p *mockPlugin) Initialize(ctx context.Context, services domains.ServiceRegistry) error {
	p.initCalled = true
	return nil
}

func (p *mockPlugin) Shutdown(ctx context.Context) error {
	p.shutdownCalled = true
	return nil
}

func (p *mockPlugin) Commands() []*cobra.Command {
	return nil
}

func (p *mockPlugin) RegisterHTTP(mux *http.ServeMux) error {
	mux.HandleFunc("/api/mock/test", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from mock"))
	})
	return nil
}

func (p *mockPlugin) RegisterWebSocket() (map[string]domains.WebSocketHandler, error) {
	return nil, nil
}

func (p *mockPlugin) Health(ctx context.Context) domains.HealthStatus {
	return domains.HealthStatus{
		Healthy: true,
		Message: "Mock plugin healthy",
		Details: map[string]interface{}{
			"test": "value",
		},
	}
}

func TestPluginServer_Metadata(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := &mockPlugin{}
	server := NewPluginServer(plugin, logger)

	resp, err := server.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err)

	assert.Equal(t, "mock", resp.Name)
	assert.Equal(t, "1.0.0", resp.Version)
	assert.Equal(t, ">= 0.1.0", resp.QntxVersion)
	assert.Equal(t, "Mock plugin for testing", resp.Description)
}

func TestPluginServer_Health(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := &mockPlugin{}
	server := NewPluginServer(plugin, logger)

	resp, err := server.Health(context.Background(), &protocol.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.Healthy)
	assert.Equal(t, "Mock plugin healthy", resp.Message)
	assert.Equal(t, "value", resp.Details["test"])
}

func TestPluginClientServer_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := &mockPlugin{}
	server := NewPluginServer(plugin, logger)

	// Start server on random port
	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer listener.Close()

	addr := listener.Addr().String()

	grpcServer := grpc.NewServer()
	protocol.RegisterDomainPluginServiceServer(grpcServer, server)

	go func() {
		grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect client
	client, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer client.Close()

	// Verify metadata
	meta := client.Metadata()
	assert.Equal(t, "mock", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)

	// Check health
	health := client.Health(context.Background())
	assert.True(t, health.Healthy)
}

func TestPluginClient_ConnectionFailure(t *testing.T) {
	// Try to connect to a non-existent server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Override the default timeout
	conn, err := grpc.DialContext(ctx, "localhost:59999",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)

	if err == nil {
		conn.Close()
		t.Skip("port 59999 unexpectedly available")
	}

	// Should fail to connect
	assert.Error(t, err)
}

func TestRemoteServiceRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	config := map[string]string{
		"key1":    "value1",
		"enabled": "true",
	}

	registry := NewRemoteServiceRegistry(
		"db://localhost:5432",
		"ats://localhost:5433",
		config,
		logger,
	)

	// Database should return nil for remote plugins
	assert.Nil(t, registry.Database())

	// ATSStore should return nil for remote plugins
	assert.Nil(t, registry.ATSStore())

	// Logger should work
	pluginLogger := registry.Logger("test")
	assert.NotNil(t, pluginLogger)

	// Config should work
	pluginConfig := registry.Config("test")
	assert.Equal(t, "value1", pluginConfig.GetString("key1"))
	assert.True(t, pluginConfig.GetBool("enabled"))
}
