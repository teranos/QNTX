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

// TestGRPCConfigSchema tests that the plugin returns correct configuration schema via gRPC.
// This verifies that the ConfigurablePlugin interface is properly implemented.
func TestGRPCConfigSchema(t *testing.T) {
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

	// Call ConfigSchema RPC
	resp, err := client.ConfigSchema(context.Background(), &protocol.Empty{})
	require.NoError(t, err, "ConfigSchema RPC failed")

	// Verify schema is not empty
	require.NotNil(t, resp.Fields, "ConfigSchema fields should not be nil")
	assert.NotEmpty(t, resp.Fields, "ConfigSchema should return configuration fields")

	// Verify expected configuration fields exist
	expectedFields := []string{
		"gopls.workspace_root",
		"gopls.enabled",
		"github.token",
		"github.default_owner",
		"github.default_repo",
	}

	for _, fieldName := range expectedFields {
		field, exists := resp.Fields[fieldName]
		assert.True(t, exists, "Expected field %s to exist in schema", fieldName)
		if exists {
			assert.NotEmpty(t, field.Type, "Field %s should have a type", fieldName)
			assert.NotEmpty(t, field.Description, "Field %s should have a description", fieldName)
		}
	}

	// Verify gopls.workspace_root specifically
	workspaceField, exists := resp.Fields["gopls.workspace_root"]
	require.True(t, exists, "gopls.workspace_root field should exist")
	assert.Equal(t, "string", workspaceField.Type, "gopls.workspace_root should be a string field")
	assert.Equal(t, ".", workspaceField.DefaultValue, "gopls.workspace_root should default to current directory")
	assert.False(t, workspaceField.Required, "gopls.workspace_root should not be required")

	// Verify gopls.enabled specifically
	enabledField, exists := resp.Fields["gopls.enabled"]
	require.True(t, exists, "gopls.enabled field should exist")
	assert.Equal(t, "boolean", enabledField.Type, "gopls.enabled should be a boolean field")
	assert.Equal(t, "true", enabledField.DefaultValue, "gopls.enabled should default to true")
	assert.False(t, enabledField.Required, "gopls.enabled should not be required")

	// Verify github.token specifically
	tokenField, exists := resp.Fields["github.token"]
	require.True(t, exists, "github.token field should exist")
	assert.Equal(t, "string", tokenField.Type, "github.token should be a string field")
	assert.False(t, tokenField.Required, "github.token should not be required")

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

// TestConfigSchema_DirectCall tests the ConfigSchema method directly without gRPC.
func TestConfigSchema_DirectCall(t *testing.T) {
	plugin := NewPlugin()

	// Call ConfigSchema directly
	schema := plugin.ConfigSchema()

	// Verify schema is not empty
	assert.NotEmpty(t, schema, "ConfigSchema should return configuration fields")

	// Verify expected fields
	expectedFields := map[string]struct {
		fieldType    string
		defaultValue string
		required     bool
	}{
		"gopls.workspace_root": {"string", ".", false},
		"gopls.enabled":        {"boolean", "true", false},
		"github.token":         {"string", "", false},
		"github.default_owner": {"string", "", false},
		"github.default_repo":  {"string", "", false},
	}

	for fieldName, expected := range expectedFields {
		field, exists := schema[fieldName]
		require.True(t, exists, "Expected field %s to exist in schema", fieldName)

		assert.Equal(t, expected.fieldType, field.Type, "Field %s should have type %s", fieldName, expected.fieldType)
		assert.Equal(t, expected.defaultValue, field.DefaultValue, "Field %s should have default value %s", fieldName, expected.defaultValue)
		assert.Equal(t, expected.required, field.Required, "Field %s required should be %v", fieldName, expected.required)
		assert.NotEmpty(t, field.Description, "Field %s should have a description", fieldName)
	}
}
