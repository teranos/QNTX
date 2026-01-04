// Package qntxcode provides the built-in code domain plugin for QNTX.
//
// The code domain includes:
//   - Ixgest: Git repository and dependency ingestion
//   - VCS: GitHub PR workflow integration
//   - Language Server: gopls for Go code intelligence
//   - UI: Code editor and browser
package qntxcode

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin"
)

// Plugin is the code domain plugin implementation
type Plugin struct {
	services plugin.ServiceRegistry
}

// NewPlugin creates a new code domain plugin
func NewPlugin() *Plugin {
	return &Plugin{}
}

// Metadata returns information about the code domain plugin
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "code",
		Version:     "0.1.0",
		QNTXVersion: ">= 0.1.0",
		Description: "Software development domain (git, GitHub, gopls, code editor)",
		Author:      "QNTX Team",
		License:     "MIT",
	}
}

// Initialize initializes the code domain plugin
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.services = services

	logger := services.Logger("code")
	logger.Info("Code domain plugin initialized")

	return nil
}

// Shutdown shuts down the code domain plugin
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("code")
	logger.Info("Code domain plugin shutting down")

	return nil
}

// RegisterHTTP registers HTTP handlers for the code domain
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// RegisterWebSocket registers WebSocket handlers for the code domain
func (p *Plugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	handlers := make(map[string]plugin.WebSocketHandler)

	// Issue #127: Integrate plugin WebSocket handlers into server
	// - /gopls - gopls language server protocol

	return handlers, nil
}

// Health returns the health status of the code domain plugin
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	// Issue #131: Implement health checks for code domain plugin
	// Should verify: gopls service, database connectivity, optional GitHub API

	return plugin.HealthStatus{
		Healthy: true,
		Message: "Code domain operational",
		Details: make(map[string]interface{}),
	}
}

// Register the code domain plugin on import
func init() {
	// Plugin will be registered when the registry is initialized
	// This is done in main.go after creating the registry
}
