// Package qntxcode provides the code domain plugin for QNTX.
//
// The code domain includes:
//   - Ixgest: Git repository and dependency ingestion
//   - VCS: GitHub PR workflow integration
//   - Language Server: gopls for Go code intelligence
//   - UI: Code editor and browser
//
// This plugin runs as an external gRPC process. Build with:
//
//	go build ./qntx-code/cmd/qntx-code-plugin
//
// Then install to ~/.qntx/plugins/ or add to plugin.paths in am.toml.
package qntxcode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/qntx-code/langserver/gopls"
)

// Plugin is the code domain plugin implementation
// Implements plugin.PausablePlugin for runtime pause/resume support
type Plugin struct {
	services     plugin.ServiceRegistry
	goplsService *gopls.Service // Go language server for code intelligence
	paused       bool           // Whether the plugin is currently paused
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

	// Initialize gopls service for Go code intelligence
	config := services.Config("code")
	workspaceRoot := config.GetString("gopls.workspace_root")
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	goplsService, err := gopls.NewService(gopls.Config{
		WorkspaceRoot: workspaceRoot,
		Logger:        logger,
	})
	if err != nil {
		logger.Warnw("Failed to create gopls service, Go code intelligence disabled", "error", err)
		p.goplsService = nil
		p.attestGoplsStatus("failed", workspaceRoot, err.Error())
	} else {
		// Initialize gopls with timeout
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := goplsService.Initialize(initCtx); err != nil {
			logger.Warnw("Failed to initialize gopls, Go code intelligence disabled", "error", err)
			p.goplsService = nil
			p.attestGoplsStatus("failed", workspaceRoot, err.Error())
		} else {
			p.goplsService = goplsService
			logger.Infow(fmt.Sprintf("gopls service initialized (workspace: %s)", workspaceRoot))
			p.attestGoplsStatus("initialized", workspaceRoot, "")
		}
	}

	logger.Info("Code domain plugin initialized")
	return nil
}

// Shutdown shuts down the code domain plugin
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("code")

	// Shutdown gopls service
	if p.goplsService != nil {
		logger.Info("Stopping gopls service")
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := p.goplsService.Shutdown(shutdownCtx); err != nil {
			logger.Warnw("Failed to shutdown gopls cleanly", "error", err)
		} else {
			logger.Info("gopls service stopped")
		}
	}

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

	// Register gopls language server WebSocket handler
	handlers["/gopls"] = &goplsWebSocketHandler{plugin: p}

	return handlers, nil
}

// Health returns the health status of the code domain plugin
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	// Issue #131: Implement health checks for code domain plugin
	// Should verify: gopls service, database connectivity, optional GitHub API

	message := "Code domain operational"
	if p.paused {
		message = "Code domain paused"
	}

	details := map[string]interface{}{
		"gopls_available": p.goplsService != nil,
	}

	return plugin.HealthStatus{
		Healthy: true,     // Paused is intentional, not a failure
		Paused:  p.paused, // Separate field for pause state
		Message: message,
		Details: details,
	}
}

// Pause temporarily suspends the code domain plugin operations
// Implements plugin.PausablePlugin
func (p *Plugin) Pause(ctx context.Context) error {
	logger := p.services.Logger("code")

	if p.paused {
		return fmt.Errorf("code plugin is already paused")
	}

	// Pause gopls service if available
	if p.goplsService != nil {
		logger.Info("Pausing gopls service")
		// gopls doesn't have a pause method, so we just mark as paused
		// HTTP handlers will check p.paused and return 503
	}

	p.paused = true
	logger.Info("Code domain plugin paused")
	return nil
}

// Resume restores the code domain plugin to active operation
// Implements plugin.PausablePlugin
func (p *Plugin) Resume(ctx context.Context) error {
	logger := p.services.Logger("code")

	if !p.paused {
		return fmt.Errorf("code plugin is not paused")
	}

	p.paused = false
	logger.Info("Code domain plugin resumed")
	return nil
}

// IsPaused returns whether the plugin is currently paused
func (p *Plugin) IsPaused() bool {
	return p.paused
}

// ConfigSchema returns the configuration schema for UI-based configuration.
// Implements plugin.ConfigurablePlugin.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"gopls.workspace_root": {
			Type:         "string",
			Description:  "Root directory for gopls workspace. Defaults to current directory.",
			DefaultValue: ".",
			Required:     false,
		},
		"gopls.enabled": {
			Type:         "boolean",
			Description:  "Enable gopls Go language server for code intelligence.",
			DefaultValue: "true",
			Required:     false,
		},
		"github.token": {
			Type:        "string",
			Description: "GitHub personal access token for API operations (PRs, issues).",
			Required:    false,
		},
		"github.default_owner": {
			Type:        "string",
			Description: "Default GitHub repository owner/organization.",
			Required:    false,
		},
		"github.default_repo": {
			Type:        "string",
			Description: "Default GitHub repository name.",
			Required:    false,
		},
	}
}

// Verify Plugin implements ConfigurablePlugin at compile time
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)

// attestGoplsStatus creates an attestation for gopls initialization status
func (p *Plugin) attestGoplsStatus(status, workspace, errMsg string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	attrs := map[string]interface{}{
		"workspace": workspace,
	}
	if errMsg != "" {
		attrs["error"] = errMsg
	}

	cmd := &types.AsCommand{
		Subjects:   []string{"gopls"},
		Predicates: []string{status},
		Contexts:   []string{"code-domain"},
		Attributes: attrs,
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("code")
		logger.Debugw("Failed to create gopls status attestation", "status", status, "error", err)
	}
}

// Register the code domain plugin on import
func init() {
	// Plugin will be registered when the registry is initialized
	// This is done in main.go after creating the registry
}
