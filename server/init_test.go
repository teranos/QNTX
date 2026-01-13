package server

import (
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
)

// TestServerInitialization verifies that NewQNTXServer correctly initializes all dependencies
func TestServerInitialization(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	server, err := NewQNTXServer(db, "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify critical dependencies are initialized
	if server.db == nil {
		t.Error("Database not initialized")
	}
	if server.builder == nil {
		t.Error("Graph builder not initialized")
	}
	if server.langService == nil {
		t.Error("Language service not initialized")
	}
	if server.usageTracker == nil {
		t.Error("Usage tracker not initialized")
	}
	if server.budgetTracker == nil {
		t.Error("Budget tracker not initialized")
	}
	if server.daemon == nil {
		t.Error("Daemon not initialized")
	}
	if server.logger == nil {
		t.Error("Logger not initialized")
	}

	// pluginManager and pluginRegistry may be nil if no plugins configured - that's OK
	// Just verify they're accessible fields
	_ = server.pluginManager
	_ = server.pluginRegistry
}

// TestServerWithPluginManager verifies plugin manager is correctly wired up when set globally
func TestServerWithPluginManager(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Create and set a plugin manager globally (simulating main.go behavior)
	logger := zap.NewNop().Sugar()
	manager := grpcplugin.NewPluginManager(logger)
	grpcplugin.SetDefaultPluginManager(manager)
	t.Cleanup(func() {
		grpcplugin.SetDefaultPluginManager(nil) // Clean up global state
	})

	server, err := NewQNTXServer(db, "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify plugin manager was picked up from global storage
	if server.pluginManager == nil {
		t.Error("Plugin manager should be set when global manager exists")
	}
	if server.pluginManager != manager {
		t.Error("Plugin manager should match the globally set manager")
	}
}

// TestServerWithPluginRegistry verifies plugin registry field exists
func TestServerWithPluginRegistry(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	server, err := NewQNTXServer(db, "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Note: pluginRegistry gets set later in initialization via plugin.GetDefaultRegistry()
	// This test just verifies the field exists and is accessible
	// The registry may or may not be set depending on test execution order
	_ = server.pluginRegistry
}

// TestServerServicesRegistry verifies services registry is properly initialized
// This test documents the fix for a nil pointer panic that occurred when reinitializing plugins
// The panic happened in plugin/grpc/client.go:107 when services.Config() was called with nil services
func TestServerServicesRegistry(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Set plugin manager globally (simulating main.go behavior)
	logger := zap.NewNop().Sugar()
	manager := grpcplugin.NewPluginManager(logger)
	grpcplugin.SetDefaultPluginManager(manager)
	t.Cleanup(func() {
		grpcplugin.SetDefaultPluginManager(nil)
	})

	// Get default registry if it exists (may be set from other tests)
	// If not set, services will be nil which is expected
	existingRegistry := plugin.GetDefaultRegistry()

	server, err := NewQNTXServer(db, "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// If plugin registry exists, services should be initialized
	// This prevents nil pointer panics during plugin reinitialization
	if existingRegistry != nil && server.services == nil {
		t.Error("Services registry should be set when plugin registry exists (prevents nil pointer in ReinitializePlugin)")
	}
}

// TestServerInitializationWithInvalidDB verifies proper error handling
func TestServerInitializationWithInvalidDB(t *testing.T) {
	_, err := NewQNTXServer(nil, "test.db", 1)
	if err == nil {
		t.Error("Expected error when creating server with nil database")
	}
	if err.Error() != "database connection cannot be nil" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestServerInitializationWithInvalidVerbosity verifies verbosity validation
func TestServerInitializationWithInvalidVerbosity(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	tests := []struct {
		verbosity int
		wantErr   bool
	}{
		{-1, true}, // Too low
		{0, false}, // Valid
		{1, false}, // Valid
		{4, false}, // Valid
		{5, true},  // Too high
		{10, true}, // Way too high
	}

	for _, tt := range tests {
		_, err := NewQNTXServer(db, "test.db", tt.verbosity)
		if tt.wantErr && err == nil {
			t.Errorf("verbosity=%d: expected error, got nil", tt.verbosity)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("verbosity=%d: unexpected error: %v", tt.verbosity, err)
		}
	}
}
