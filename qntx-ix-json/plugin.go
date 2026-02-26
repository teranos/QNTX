// Package qntxixjson provides a generic JSON API ingestion plugin for QNTX.
//
// The ix-json plugin fetches JSON from HTTP APIs and maps responses to QNTX attestations.
// It operates in three modes:
//   - Data-shaping: Explore API response structure and configure mappings
//   - Paused: Schedule suspended, allows reconfiguration
//   - Active-running: Periodically polls API and creates attestations
//
// Build with:
//
//	go build ./qntx-ix-json/cmd/qntx-ix-json-plugin
//
// Then install to ~/.qntx/plugins/ or add to plugin.paths in am.toml.
package qntxixjson

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// OperationMode represents the current operational mode of the plugin.
type OperationMode string

const (
	ModeDataShaping   OperationMode = "data-shaping"
	ModePaused        OperationMode = "paused"
	ModeActiveRunning OperationMode = "active-running"
)

// MappingConfig defines how JSON fields map to Attestation SPC + attributes.
type MappingConfig struct {
	SubjectPath   string            `json:"subject_path"`   // JSON path for Subject
	PredicatePath string            `json:"predicate_path"` // JSON path for Predicate
	ContextPath   string            `json:"context_path"`   // JSON path for Context
	RichFields    []string          `json:"rich_fields"`    // Fields marked as rich strings
	KeyRemapping  map[string]string `json:"key_remapping"`  // Old key -> new key
}

// Plugin is the ix-json plugin implementation.
type Plugin struct {
	services plugin.ServiceRegistry

	mu             sync.RWMutex
	mode           OperationMode             // Protected by mu
	httpClient     *http.Client              // Protected by mu
	apiURL         string                    // Protected by mu
	authToken      string                    // Protected by mu
	glyphResponses map[string][]byte         // Protected by mu - per-glyph cached response
	glyphMappings  map[string]*MappingConfig // Protected by mu - per-glyph mapping
}

// NewPlugin creates a new ix-json plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		httpClient:     &http.Client{},
		mode:           ModeDataShaping,
		glyphResponses: make(map[string][]byte),
		glyphMappings:  make(map[string]*MappingConfig),
	}
}

// Metadata returns information about the ix-json plugin.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "ix-json",
		Version:     "0.1.4",
		QNTXVersion: ">= 0.1.0",
		Description: "Generic JSON API ingestion with configurable mapping to attestations",
		Author:      "QNTX Team",
		License:     "MIT",
	}
}

// Initialize initializes the ix-json plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.services = services
	logger := services.Logger("ix-json")

	// Load configuration from attestations ONLY (no TOML config)
	config := p.loadAttestedConfig(ctx)

	apiURL := ""
	authToken := ""
	intervalSecs := 0

	if config != nil {
		if url, ok := config["api_url"].(string); ok {
			apiURL = url
		}
		if token, ok := config["auth_token"].(string); ok {
			authToken = token
		}
		if interval, ok := config["poll_interval_seconds"].(float64); ok {
			intervalSecs = int(interval)
		}
	}

	p.mu.Lock()
	p.apiURL = apiURL
	p.authToken = authToken
	if intervalSecs > 0 {
		p.mode = ModeActiveRunning
	} else {
		p.mode = ModeDataShaping
	}
	p.mu.Unlock()

	if apiURL == "" {
		logger.Info("ix-json plugin initialized (unconfigured - use UI to configure)")
	} else {
		logger.Infow("ix-json plugin initialized from attestations",
			"api_url", apiURL,
			"mode", p.mode,
			"poll_interval_seconds", intervalSecs,
		)
	}

	return nil
}

// loadAttestedConfig loads the most recent plugin-wide configuration attestation (deprecated - use per-glyph config).
func (p *Plugin) loadAttestedConfig(ctx context.Context) map[string]interface{} {
	store := p.services.ATSStore()
	if store == nil {
		return nil
	}

	// Query for most recent configuration attestation
	// Subject: ix-json-plugin, Predicate: configured
	attestations, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{"ix-json-plugin"},
		Predicates: []string{"configured"},
		Limit:      1,
	})

	if err != nil || len(attestations) == 0 {
		return nil
	}

	// Return the most recent one (sorted by ID descending in query)
	return attestations[0].Attributes
}

// loadGlyphConfig loads the configuration for a specific glyph instance.
func (p *Plugin) loadGlyphConfig(ctx context.Context, glyphID string) map[string]interface{} {
	if glyphID == "" {
		return nil
	}

	store := p.services.ATSStore()
	if store == nil {
		return nil
	}

	// Subject: ix-json-glyph-{glyphID}, Predicate: configured
	subject := fmt.Sprintf("ix-json-glyph-%s", glyphID)

	attestations, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Limit:      1,
	})

	if err != nil {
		p.services.Logger("ix-json").Errorw("Failed to load glyph config",
			"glyph_id", glyphID, "error", err)
		return nil
	}

	if len(attestations) == 0 {
		return nil
	}

	return attestations[0].Attributes
}

// Shutdown shuts down the ix-json plugin.
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("ix-json")
	logger.Info("ix-json plugin shutting down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the ix-json plugin.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// RegisterWebSocket registers WebSocket handlers.
func (p *Plugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return nil, nil
}

// Health returns the health status of the ix-json plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	p.mu.RLock()
	mode := p.mode
	apiURL := p.apiURL
	p.mu.RUnlock()

	message := fmt.Sprintf("ix-json plugin operational (mode: %s)", mode)

	details := map[string]interface{}{
		"mode":    mode,
		"api_url": apiURL,
	}

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  mode == ModePaused,
		Message: message,
		Details: details,
	}
}

// Pause temporarily suspends the plugin operations.
func (p *Plugin) Pause(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.mode == ModePaused {
		return fmt.Errorf("ix-json plugin is already paused")
	}
	p.mode = ModePaused
	p.services.Logger("ix-json").Info("ix-json plugin paused")
	return nil
}

// Resume restores the plugin to active operation.
func (p *Plugin) Resume(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.mode != ModePaused {
		return fmt.Errorf("ix-json plugin is not paused")
	}
	p.mode = ModeActiveRunning
	p.services.Logger("ix-json").Info("ix-json plugin resumed")
	return nil
}

// ConfigSchema returns the configuration schema for UI-based configuration.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"api_url": {
			Type:        "string",
			Description: "API endpoint URL that returns JSON data.",
			Required:    true,
		},
		"auth_token": {
			Type:        "string",
			Description: "Optional Bearer token for API authentication.",
			Required:    false,
		},
		"poll_interval_seconds": {
			Type:         "int",
			Description:  "Polling interval in seconds (0 = data-shaping mode, no auto-polling).",
			DefaultValue: "0",
			Required:     false,
			MinValue:     "0",
		},
	}
}

// RegisterGlyphs returns custom glyph type definitions provided by this plugin.
func (p *Plugin) RegisterGlyphs() []plugin.GlyphDef {
	return []plugin.GlyphDef{
		{
			Symbol:        "🔄",
			Title:         "JSON API Ingestor",
			Label:         "ix-json",
			ContentPath:   "/ix-glyph",
			CSSPath:       "/ix-glyph.css",
			DefaultWidth:  600,
			DefaultHeight: 700,
		},
	}
}

// GetSchedules returns the schedules this plugin wants QNTX to create.
func (p *Plugin) GetSchedules() []*protocol.ScheduleInfo {
	config := p.services.Config("ix-json")
	interval := int32(config.GetInt("poll_interval_seconds"))

	if interval <= 0 {
		return nil // Data-shaping mode, no schedule
	}

	return []*protocol.ScheduleInfo{
		{
			HandlerName:      "ix-json.poll",
			IntervalSeconds:  interval,
			EnabledByDefault: true,
			Description:      "Poll JSON API and create attestations",
		},
	}
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{"ix-json.poll"}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) (result []byte, err error) {
	switch handlerName {
	case "ix-json.poll":
		if err := p.pollAndIngest(ctx); err != nil {
			return nil, err
		}

		resultData := map[string]string{
			"status": "Poll completed successfully",
		}
		return json.Marshal(resultData)

	default:
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
