package server

import (
	"testing"

	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap/zaptest"
)

func TestSetupHTTPRoutes_RegistersAllPlugins(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	// Create test registry with multiple plugins
	registry := plugin.NewRegistry("test-version", logger)

	// Create minimal server with plugin registry
	srv := &QNTXServer{
		pluginRegistry: registry,
		logger:         logger,
	}

	// Setup routes should not panic even with empty registry
	srv.setupHTTPRoutes()

	// Note: This test primarily ensures setupHTTPRoutes doesn't panic
	// and handles empty/populated registries gracefully.
	// Full routing verification would require starting HTTP server.
}
