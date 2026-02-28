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
//
// TODO(#611): Separate into own Go module (currently uses root go.mod)
package qntxatproto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// Plugin is the AT Protocol domain plugin implementation.
type Plugin struct {
	plugin.Base

	mu     sync.RWMutex
	client *xrpc.Client // Protected by mu
	did    string       // Protected by mu

	// Stored for re-authentication when refresh token also expires
	pdsHost     string
	identifier  string
	appPassword string
}

// NewPlugin creates a new AT Protocol domain plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "atproto",
			Version:     "0.3.0",
			QNTXVersion: ">= 0.1.0",
			Description: "AT Protocol integration (Bluesky) with auto-scheduled timeline sync",
			Author:      "QNTX Team",
			License:     "MIT",
		}),
	}
}

// Initialize initializes the AT Protocol domain plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	logger := services.Logger("atproto")

	config := services.Config("atproto")
	pdsHost := config.GetString("pds_host")
	identifier := config.GetString("identifier")
	appPassword := config.GetString("app_password")

	if pdsHost == "" {
		pdsHost = "https://bsky.social"
	}

	p.pdsHost = pdsHost
	p.identifier = identifier
	p.appPassword = appPassword

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

			logger.Infow("Authenticated with PDS",
				"did", did,
				"pds_host", pdsHost,
			)
			p.attestSessionStatus("authenticated", pdsHost, did, "")
		}
	} else {
		logger.Infow("No credentials configured, running unauthenticated",
			"pds_host", pdsHost,
		)
	}

	// Attest type definitions for searchable fields
	store := services.ATSStore()
	if store != nil {
		if err := EnsureTypes(store, "atproto", TimelinePost); err != nil {
			logger.Warnw("Failed to attest type definitions", "error", err)
		}
	}

	logger.Info("AT Protocol domain plugin initialized")
	return nil
}

// Shutdown shuts down the AT Protocol domain plugin.
func (p *Plugin) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	p.client = nil
	p.did = ""
	p.mu.Unlock()

	return p.Base.Shutdown(ctx)
}

// RegisterHTTP registers HTTP handlers for the atproto domain.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// Health returns the health status of the atproto domain plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	status := p.Base.Health(ctx)

	p.mu.RLock()
	authenticated := p.client != nil
	did := p.did
	p.mu.RUnlock()

	details := map[string]interface{}{
		"authenticated": authenticated,
	}
	if did != "" {
		details["did"] = did
	}
	status.Details = details

	return status
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
		"timeline_sync_limit": {
			Type:         "int",
			Description:  "Number of posts to fetch per timeline sync (1-100).",
			DefaultValue: "50",
			Required:     false,
		},
		"timeline_sync_interval_seconds": {
			Type:         "int",
			Description:  "Timeline sync interval in seconds (0 = disabled). Plugin auto-creates schedule.",
			DefaultValue: "0",
			Required:     false,
		},
	}
}

// doWithRefresh executes an operation with the authenticated client. If the access token
// has expired, it refreshes the session and retries once. If the refresh token is also
// expired, it falls back to full re-authentication with stored credentials.
func (p *Plugin) doWithRefresh(ctx context.Context, op func(*xrpc.Client) error) error {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()

	if client == nil {
		return errors.New("not authenticated")
	}

	err := op(client)
	if err == nil || !isExpiredToken(err) {
		return err
	}

	logger := p.Services().Logger("atproto")
	logger.Info("Access token expired, refreshing session")

	// Try refresh token first
	p.mu.Lock()
	refreshErr := refreshSession(ctx, p.client)
	if refreshErr == nil {
		logger.Info("Session refreshed successfully")
		client = p.client
		p.mu.Unlock()
		return op(client)
	}
	p.mu.Unlock()

	logger.Warnw("Refresh token failed, re-authenticating", "error", refreshErr)

	// Fall back to full re-authentication
	newClient, did, authErr := createSession(ctx, p.pdsHost, p.identifier, p.appPassword)
	if authErr != nil {
		return errors.Wrap(authErr, "re-authentication failed after token expiry")
	}

	p.mu.Lock()
	p.client = newClient
	p.did = did
	p.mu.Unlock()

	logger.Infow("Re-authenticated with PDS", "did", did)
	return op(newClient)
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

// GetSchedules returns the schedules this plugin wants QNTX to create.
// Called during initialization to auto-create Pulse scheduled jobs.
func (p *Plugin) GetSchedules() []*protocol.ScheduleInfo {
	config := p.Services().Config("atproto")
	interval := int32(config.GetInt("timeline_sync_interval_seconds"))

	logger := p.Services().Logger("atproto")
	logger.Infow("GetSchedules called",
		"interval", interval,
		"all_keys", config.GetKeys(),
	)

	// If interval is 0, don't create schedule (disabled)
	if interval <= 0 {
		logger.Warnw("Timeline sync disabled (interval <= 0)", "interval", interval)
		return nil
	}

	return []*protocol.ScheduleInfo{
		{
			HandlerName:      "atproto.timeline-sync",
			IntervalSeconds:  interval,
			EnabledByDefault: true,
			Description:      "Sync Bluesky timeline to local attestations",
		},
	}
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{"atproto.timeline-sync"}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) (result []byte, logs []*protocol.JobLogEntry, err error) {
	switch handlerName {
	case "atproto.timeline-sync":
		logs = append(logs, jobLog("info", "timeline-sync", "Starting timeline sync"))

		if err := p.syncTimeline(ctx, jobID); err != nil {
			logs = append(logs, jobLog("error", "timeline-sync", fmt.Sprintf("Sync failed: %v", err)))
			return nil, logs, err
		}

		logs = append(logs, jobLog("info", "timeline-sync", "Timeline sync completed"))

		resultData := map[string]string{
			"status": "Timeline sync completed",
		}
		result, err := json.Marshal(resultData)
		return result, logs, err

	default:
		return nil, nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
}

func jobLog(level, stage, message string) *protocol.JobLogEntry {
	return &protocol.JobLogEntry{
		Timestamp: time.Now().Format(time.RFC3339),
		Level:     level,
		Stage:     stage,
		Message:   message,
	}
}

// RegisterGlyphs returns custom glyph type definitions provided by this plugin.
// Implements the UIPlugin interface.
func (p *Plugin) RegisterGlyphs() []plugin.GlyphDef {
	return []plugin.GlyphDef{
		{
			Symbol:        "🦋",
			Title:         "AT Protocol Feed",
			Label:         "atproto-feed",
			ContentPath:   "/feed-glyph",
			CSSPath:       "/feed-glyph.css",
			DefaultWidth:  500,
			DefaultHeight: 600,
		},
	}
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
