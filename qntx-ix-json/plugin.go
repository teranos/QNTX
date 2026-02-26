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
	glyphResponses map[string][]byte         // Protected by mu - per-glyph cached response
	glyphMappings  map[string]*MappingConfig // Protected by mu - per-glyph mapping
	glyphSchedules map[string]string         // Protected by mu - per-glyph schedule ID (glyphID → scheduleID)
}

// NewPlugin creates a new ix-json plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		httpClient:     &http.Client{},
		mode:           ModeDataShaping,
		glyphResponses: make(map[string][]byte),
		glyphMappings:  make(map[string]*MappingConfig),
		glyphSchedules: make(map[string]string),
	}
}

// Metadata returns information about the ix-json plugin.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "ix-json",
		Version:     "0.3.0",
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

// Shutdown shuts down the ix-json plugin, deleting all active schedules.
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("ix-json")

	p.mu.Lock()
	schedSvc := p.services.Schedule()
	for glyphID, scheduleID := range p.glyphSchedules {
		if schedSvc != nil {
			if err := schedSvc.Delete(scheduleID); err != nil {
				logger.Warnw("Failed to delete schedule on shutdown",
					"glyph_id", glyphID, "schedule_id", scheduleID, "error", err)
			} else {
				logger.Infow("Deleted schedule on shutdown", "glyph_id", glyphID, "schedule_id", scheduleID)
			}
		}
	}
	p.glyphSchedules = make(map[string]string)
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
	activeSchedules := len(p.glyphSchedules)
	p.mu.RUnlock()

	message := fmt.Sprintf("ix-json plugin operational (mode: %s, active schedules: %d)", mode, activeSchedules)

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  mode == ModePaused,
		Message: message,
		Details: map[string]any{
			"mode":             mode,
			"active_schedules": activeSchedules,
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

// createSchedule creates a Pulse schedule for a glyph via ScheduleService.
func (p *Plugin) createSchedule(glyphID string, intervalSecs int) error {
	logger := p.services.Logger("ix-json")
	schedSvc := p.services.Schedule()
	if schedSvc == nil {
		return fmt.Errorf("ScheduleService not available")
	}

	// Delete existing schedule for this glyph if any
	p.mu.Lock()
	if existingID, exists := p.glyphSchedules[glyphID]; exists {
		p.mu.Unlock()
		if err := schedSvc.Delete(existingID); err != nil {
			logger.Warnw("Failed to delete previous schedule", "glyph_id", glyphID, "schedule_id", existingID, "error", err)
		}
		p.mu.Lock()
	}
	p.mu.Unlock()

	payload, _ := json.Marshal(map[string]string{"glyph_id": glyphID})
	metadata := map[string]string{
		"plugin":   "ix-json",
		"glyph_id": glyphID,
	}

	scheduleID, err := schedSvc.Create("ix-json.poll", intervalSecs, payload, metadata)
	if err != nil {
		return fmt.Errorf("failed to create schedule for glyph %s: %w", glyphID, err)
	}

	p.mu.Lock()
	p.glyphSchedules[glyphID] = scheduleID
	p.mu.Unlock()

	logger.Infow("Schedule created", "glyph_id", glyphID, "schedule_id", scheduleID, "interval_seconds", intervalSecs)
	return nil
}

// pauseSchedule pauses the Pulse schedule for a glyph.
func (p *Plugin) pauseSchedule(glyphID string) error {
	p.mu.RLock()
	scheduleID, exists := p.glyphSchedules[glyphID]
	p.mu.RUnlock()

	if !exists {
		return nil // No schedule to pause
	}

	schedSvc := p.services.Schedule()
	if schedSvc == nil {
		return fmt.Errorf("ScheduleService not available")
	}

	if err := schedSvc.Pause(scheduleID); err != nil {
		return fmt.Errorf("failed to pause schedule %s for glyph %s: %w", scheduleID, glyphID, err)
	}

	p.services.Logger("ix-json").Infow("Schedule paused", "glyph_id", glyphID, "schedule_id", scheduleID)
	return nil
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{"ix-json.poll"}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) ([]byte, []*protocol.JobLogEntry, error) {
	switch handlerName {
	case "ix-json.poll":
		var req struct {
			GlyphID string `json:"glyph_id"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, nil, fmt.Errorf("invalid poll payload: %w", err)
		}
		if req.GlyphID == "" {
			return nil, nil, fmt.Errorf("glyph_id missing from poll payload")
		}

		if err := p.pollGlyph(ctx, req.GlyphID); err != nil {
			return nil, nil, err
		}

		result, _ := json.Marshal(map[string]string{
			"status":   "poll completed",
			"glyph_id": req.GlyphID,
		})
		return result, nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
