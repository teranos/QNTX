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
	store, db := qntxtest.CreateTestStore(t)

	server, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify critical dependencies are initialized
	if server.nodeDB == nil {
		t.Error("Database not initialized")
	}
	if server.daemon == nil {
		t.Error("Daemon not initialized")
	}
	if server.logger == nil {
		t.Error("Logger not initialized")
	}
}

// TestServerWithPluginManager verifies plugin manager is correctly wired up when set globally
func TestServerWithPluginManager(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	// Create and set a plugin manager globally (simulating main.go behavior)
	logger := zap.NewNop().Sugar()
	manager := grpcplugin.NewPluginManager(logger, logger, "")
	grpcplugin.SetDefaultPluginManager(manager)
	t.Cleanup(func() {
		grpcplugin.SetDefaultPluginManager(nil) // Clean up global state
	})

	server, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
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

// The node runs with the registry cmd/qntx set, the one TestMain sets here.
func TestServerWithPluginRegistry(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	server, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server.pluginRegistry != plugin.GetDefaultRegistry() {
		t.Error("the node runs with a registry other than the one it was given")
	}
}

// TestServerServicesRegistry verifies services registry is properly initialized
// This test documents the fix for a nil pointer panic that occurred when reinitializing plugins
// The panic happened in plugin/grpc/client.go:107 when services.Config() was called with nil services
func TestServerServicesRegistry(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	// Set plugin manager globally (simulating main.go behavior)
	logger := zap.NewNop().Sugar()
	manager := grpcplugin.NewPluginManager(logger, logger, "")
	grpcplugin.SetDefaultPluginManager(manager)
	t.Cleanup(func() {
		grpcplugin.SetDefaultPluginManager(nil)
	})

	server, err := NewQNTXServer(db, servingOne(db, store), "test.db", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	if server.services == nil {
		t.Error("Services registry should be set (prevents nil pointer in ReinitializePlugin)")
	}
}

// TestServerInitializationWithInvalidDB verifies proper error handling
func TestServerInitializationWithInvalidDB(t *testing.T) {
	// The database is what this is about, and it is checked first.
	_, err := NewQNTXServer(nil, nil, "test.db", 1)
	if err == nil {
		t.Error("Expected error when creating server with nil database")
	}
	if err.Error() != "database connection cannot be nil" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestServerInitializationWithInvalidVerbosity verifies verbosity validation
func TestServerInitializationWithInvalidVerbosity(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

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
		_, err := NewQNTXServer(db, servingOne(db, store), "test.db", tt.verbosity)
		if tt.wantErr && err == nil {
			t.Errorf("verbosity=%d: expected error, got nil", tt.verbosity)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("verbosity=%d: unexpected error: %v", tt.verbosity, err)
		}
	}
}
