// Package qntxatproto provides the AT Protocol domain plugin for QNTX.
//
// The atproto domain includes:
//   - Authentication: XRPC session management with PDS
//   - Social: Timeline, posts, follows, likes
//   - Identity: DID/handle resolution
//   - Attestations: AT Protocol events mapped to QNTX attestation grammar
//
// This plugin runs as an external gRPC process. Build with:
//
//	go build ./qntx-atproto/cmd/qntx-atproto-plugin
//
// Then install to ~/.qntx/plugins/ or add to plugin.paths in am.toml.
package qntxatproto

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teranos/QNTX/plugin"
)

// Plugin is the AT Protocol domain plugin implementation.
// Implements plugin.PausablePlugin and plugin.ConfigurablePlugin.
type Plugin struct {
	services plugin.ServiceRegistry
	paused   bool

	mu     sync.RWMutex
	client *xrpc.Client // Authenticated XRPC client
	did    string       // Authenticated user's DID
}

// NewPlugin creates a new AT Protocol domain plugin.
func NewPlugin() *Plugin {
	return &Plugin{}
}

// Metadata returns information about the atproto domain plugin.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "atproto",
		Version:     "0.1.0",
		QNTXVersion: ">= 0.1.0",
		Description: "AT Protocol integration (Bluesky)",
		Author:      "QNTX Team",
		License:     "MIT",
	}
}

// Initialize initializes the AT Protocol domain plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.services = services
	logger := services.Logger("atproto")

	config := services.Config("atproto")
	pdsHost := config.GetString("pds_host")
	identifier := config.GetString("identifier")
	appPassword := config.GetString("app_password")

	if pdsHost == "" {
		pdsHost = "https://bsky.social"
	}

	if identifier != "" && appPassword != "" {
		client, did, err := createSession(ctx, pdsHost, identifier, appPassword)
		if err != nil {
			logger.Warnw("Failed to authenticate with PDS, running unauthenticated",
				"pds_host", pdsHost,
				"identifier", identifier,
				"error", err,
			)
			p.attestSessionStatus("failed", pdsHost, identifier, err.Error())
		} else {
			p.mu.Lock()
			p.client = client
			p.did = did
			p.mu.Unlock()

			logger.Infow(fmt.Sprintf("Authenticated with PDS (did=%s)", did),
				"pds_host", pdsHost,
			)
			p.attestSessionStatus("authenticated", pdsHost, did, "")
		}
	} else {
		logger.Infow("No credentials configured, running unauthenticated",
			"pds_host", pdsHost,
		)
	}

	logger.Info("AT Protocol domain plugin initialized")
	return nil
}

// Shutdown shuts down the AT Protocol domain plugin.
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("atproto")

	p.mu.Lock()
	p.client = nil
	p.did = ""
	p.mu.Unlock()

	logger.Info("AT Protocol domain plugin shutting down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the atproto domain.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// RegisterWebSocket registers WebSocket handlers for the atproto domain.
func (p *Plugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	// No WebSocket handlers yet — firehose subscription is future work
	return nil, nil
}

// Health returns the health status of the atproto domain plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	p.mu.RLock()
	authenticated := p.client != nil
	did := p.did
	p.mu.RUnlock()

	message := "AT Protocol domain operational"
	if p.paused {
		message = "AT Protocol domain paused"
	}

	details := map[string]interface{}{
		"authenticated": authenticated,
	}
	if did != "" {
		details["did"] = did
	}

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  p.paused,
		Message: message,
		Details: details,
	}
}

// Pause temporarily suspends the atproto domain plugin operations.
func (p *Plugin) Pause(ctx context.Context) error {
	if p.paused {
		return fmt.Errorf("atproto plugin is already paused")
	}
	p.paused = true
	p.services.Logger("atproto").Info("AT Protocol domain plugin paused")
	return nil
}

// Resume restores the atproto domain plugin to active operation.
func (p *Plugin) Resume(ctx context.Context) error {
	if !p.paused {
		return fmt.Errorf("atproto plugin is not paused")
	}
	p.paused = false
	p.services.Logger("atproto").Info("AT Protocol domain plugin resumed")
	return nil
}

// IsPaused returns whether the plugin is currently paused.
func (p *Plugin) IsPaused() bool {
	return p.paused
}

// ConfigSchema returns the configuration schema for UI-based configuration.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"pds_host": {
			Type:         "string",
			Description:  "PDS host URL for XRPC requests.",
			DefaultValue: "https://bsky.social",
			Required:     false,
		},
		"identifier": {
			Type:        "string",
			Description: "Handle or DID for authentication (e.g., user.bsky.social).",
			Required:    true,
		},
		"app_password": {
			Type:        "string",
			Description: "App password for authentication. Generate at Settings > App Passwords.",
			Required:    true,
		},
	}
}

// getClient returns the authenticated XRPC client, or nil if not authenticated.
func (p *Plugin) getClient() *xrpc.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client
}

// getDID returns the authenticated user's DID.
func (p *Plugin) getDID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.did
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
