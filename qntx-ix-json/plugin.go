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
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/pulse/async"
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
	mode           OperationMode                 // Protected by mu
	httpClient     *http.Client                  // Protected by mu
	glyphResponses map[string][]byte             // Protected by mu - per-glyph cached response
	glyphMappings  map[string]*MappingConfig     // Protected by mu - per-glyph mapping
	glyphPollers   map[string]context.CancelFunc // Protected by mu - per-glyph active poller cancel functions
}

// NewPlugin creates a new ix-json plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		httpClient:     &http.Client{},
		mode:           ModeDataShaping,
		glyphResponses: make(map[string][]byte),
		glyphMappings:  make(map[string]*MappingConfig),
		glyphPollers:   make(map[string]context.CancelFunc),
	}
}

// Metadata returns information about the ix-json plugin.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "ix-json",
		Version:     "0.2.3",
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
	logger.Info("ix-json plugin initialized (per-glyph config via attestations)")
	return nil
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

// Shutdown shuts down the ix-json plugin, stopping all pollers.
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("ix-json")

	p.mu.Lock()
	for glyphID, cancel := range p.glyphPollers {
		cancel()
		logger.Infow("Stopped poller on shutdown", "glyph_id", glyphID)
	}
	p.glyphPollers = make(map[string]context.CancelFunc)
	p.mu.Unlock()

	logger.Info("ix-json plugin shut down")
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
	activePollers := len(p.glyphPollers)
	p.mu.RUnlock()

	message := fmt.Sprintf("ix-json plugin operational (mode: %s, active pollers: %d)", mode, activePollers)

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  mode == ModePaused,
		Message: message,
		Details: map[string]any{
			"mode":           mode,
			"active_pollers": activePollers,
		},
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

// startPoller starts a per-glyph ticker that enqueues Pulse jobs on each tick.
// TEMPORARY: goroutine ticker until ScheduleService gRPC API exists (see docs/plans/plugin-runtime-schedules.md)
func (p *Plugin) startPoller(glyphID string, intervalSecs int) {
	logger := p.services.Logger("ix-json")

	p.mu.Lock()
	if cancel, exists := p.glyphPollers[glyphID]; exists {
		cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.glyphPollers[glyphID] = cancel
	p.mu.Unlock()

	logger.Infow("Starting poller", "glyph_id", glyphID, "interval_seconds", intervalSecs)

	go func() {
		ticker := time.NewTicker(time.Duration(intervalSecs) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Infow("Poller stopped", "glyph_id", glyphID)
				return
			case <-ticker.C:
				p.enqueuePollJob(glyphID)
			}
		}
	}()
}

// enqueuePollJob enqueues a Pulse job to poll a specific glyph.
func (p *Plugin) enqueuePollJob(glyphID string) {
	logger := p.services.Logger("ix-json")
	queue := p.services.Queue()
	if queue == nil {
		logger.Errorw("Queue not available, polling directly", "glyph_id", glyphID)
		if err := p.pollGlyph(context.Background(), glyphID); err != nil {
			logger.Errorw("Direct poll failed", "glyph_id", glyphID, "error", err)
		}
		return
	}

	payload, _ := json.Marshal(map[string]string{"glyph_id": glyphID})
	job, err := async.NewJobWithPayload("ix-json.poll", "ix-json-glyph-"+glyphID, payload, 1, 0, "ix-json")
	if err != nil {
		logger.Errorw("Failed to create poll job", "glyph_id", glyphID, "error", err)
		return
	}

	if err := queue.Enqueue(job); err != nil {
		logger.Errorw("Failed to enqueue poll job", "glyph_id", glyphID, "error", err)
	}
}

// stopPoller stops the polling goroutine for a glyph.
func (p *Plugin) stopPoller(glyphID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if cancel, exists := p.glyphPollers[glyphID]; exists {
		cancel()
		delete(p.glyphPollers, glyphID)
		p.services.Logger("ix-json").Infow("Poller cancelled", "glyph_id", glyphID)
	}
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{"ix-json.poll"}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) ([]byte, error) {
	switch handlerName {
	case "ix-json.poll":
		var req struct {
			GlyphID string `json:"glyph_id"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("invalid poll payload: %w", err)
		}
		if req.GlyphID == "" {
			return nil, fmt.Errorf("glyph_id missing from poll payload")
		}

		if err := p.pollGlyph(ctx, req.GlyphID); err != nil {
			return nil, err
		}

		return json.Marshal(map[string]string{
			"status":   "poll completed",
			"glyph_id": req.GlyphID,
		})

	default:
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
