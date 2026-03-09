// Package qntxixjson provides a generic JSON API ingestion plugin for QNTX.
//
// The ix-json plugin fetches JSON from HTTP APIs and maps responses to QNTX attestations.
// It operates in two modes:
//   - Paused: Schedule suspended, allows reconfiguration
//   - Active-running: Periodically polls API and creates attestations
//
// Build with:
//
//	go build ./qntx-plugins/ix-json/cmd/qntx-ix-json-plugin
//
// Then install to ~/.qntx/plugins/ or add to plugin.paths in am.toml.
package qntxixjson

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/ats"
	atstypes "github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

//go:embed web/ix-glyph-module.js
var ixGlyphModuleJS []byte

// OperationMode represents the current operational mode of a glyph.
type OperationMode string

const (
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
	plugin.Base

	glyphMu        sync.RWMutex
	httpClient     *http.Client
	glyphResponses map[string][]byte         // Protected by glyphMu - per-glyph cached response
	glyphMappings  map[string]*MappingConfig // Protected by glyphMu - per-glyph mapping
	glyphSchedules map[string]string         // Protected by glyphMu - per-glyph schedule ID (glyphID → scheduleID)
}

// NewPlugin creates a new ix-json plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "ix-json",
			Version:     "0.4.1",
			QNTXVersion: ">= 0.1.0",
			Description: "Generic JSON API ingestion with configurable mapping to attestations",
			Author:      "QNTX Team",
			License:     "MIT",
		}),
		httpClient:     &http.Client{},
		glyphResponses: make(map[string][]byte),
		glyphMappings:  make(map[string]*MappingConfig),
		glyphSchedules: make(map[string]string),
	}
}

// Initialize initializes the ix-json plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	services.Logger("ix-json").Info("ix-json plugin initialized (per-glyph config via attestations)")
	return nil
}

// loadGlyphConfig loads the configuration for a specific glyph instance.
func (p *Plugin) loadGlyphConfig(ctx context.Context, glyphID string) map[string]interface{} {
	if glyphID == "" {
		return nil
	}

	store := p.Services().ATSStore()
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
		p.Services().Logger("ix-json").Errorw("Failed to load glyph config",
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
	logger := p.Services().Logger("ix-json")

	// Snapshot and clear under lock, then delete without holding it.
	// Delete is a gRPC call that could block during concurrent shutdown.
	p.glyphMu.Lock()
	schedules := make(map[string]string, len(p.glyphSchedules))
	for k, v := range p.glyphSchedules {
		schedules[k] = v
	}
	p.glyphSchedules = make(map[string]string)
	p.glyphMu.Unlock()

	schedSvc := p.Services().Schedule()
	for glyphID, scheduleID := range schedules {
		if schedSvc != nil {
			if err := schedSvc.Delete(scheduleID); err != nil {
				logger.Warnw("Failed to delete schedule on shutdown",
					"glyph_id", glyphID, "schedule_id", scheduleID, "error", err)
			} else {
				logger.Infow("Deleted schedule on shutdown", "glyph_id", glyphID, "schedule_id", scheduleID)
			}
		}
	}

	logger.Info("ix-json plugin shut down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the ix-json plugin.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// Health returns the health status of the ix-json plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	p.glyphMu.RLock()
	activeSchedules := len(p.glyphSchedules)
	p.glyphMu.RUnlock()

	paused := p.IsPaused()
	mode := ModeActiveRunning
	if paused {
		mode = ModePaused
	}

	message := fmt.Sprintf("ix-json plugin operational (mode: %s, active schedules: %d)", mode, activeSchedules)

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  paused,
		Message: message,
		Details: map[string]any{
			"mode":             mode,
			"active_schedules": activeSchedules,
		},
	}
}

// ConfigSchema returns the configuration schema for UI-based configuration.
// All ix-json config is per-glyph (via glyph UI + attestations), not plugin-level.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{}
}

// RegisterGlyphs returns custom glyph type definitions provided by this plugin.
func (p *Plugin) RegisterGlyphs() []plugin.GlyphDef {
	return []plugin.GlyphDef{
		{
			Symbol:        "🔄",
			Title:         "JSON API Ingestor",
			Label:         "ix-json",
			ModulePath:    "/ix-glyph-module.js",
			DefaultWidth:  600,
			DefaultHeight: 700,
		},
	}
}

// createSchedule creates a Pulse schedule for a glyph via ScheduleService.
// Deletes any existing schedule for this glyph (from memory or attestation) before creating.
func (p *Plugin) createSchedule(glyphID string, intervalSecs int) error {
	logger := p.Services().Logger("ix-json")
	schedSvc := p.Services().Schedule()
	if schedSvc == nil {
		return fmt.Errorf("ScheduleService not available")
	}

	// Find existing schedule ID: check in-memory first, then attestation
	p.glyphMu.RLock()
	existingID := p.glyphSchedules[glyphID]
	p.glyphMu.RUnlock()

	if existingID == "" {
		existingID = p.loadScheduleID(glyphID)
	}

	if existingID != "" {
		if err := schedSvc.Delete(existingID); err != nil {
			logger.Warnw("Failed to delete previous schedule", "glyph_id", glyphID, "schedule_id", existingID, "error", err)
		}
	}

	payload, _ := json.Marshal(map[string]string{"glyph_id": glyphID})
	metadata := map[string]string{
		"plugin":   "ix-json",
		"glyph_id": glyphID,
	}

	scheduleID, err := schedSvc.Create("ix-json.poll", intervalSecs, payload, metadata)
	if err != nil {
		return fmt.Errorf("failed to create schedule for glyph %s: %w", glyphID, err)
	}

	p.glyphMu.Lock()
	p.glyphSchedules[glyphID] = scheduleID
	p.glyphMu.Unlock()

	// Persist schedule ID so it survives plugin restarts
	p.saveScheduleID(glyphID, scheduleID)

	logger.Infow("Schedule created", "glyph_id", glyphID, "schedule_id", scheduleID, "interval_seconds", intervalSecs)
	return nil
}

// loadScheduleID loads a persisted schedule ID from the glyph's schedule attestation.
// Uses predicate "scheduled" (separate from "configured") to avoid clobbering glyph config.
func (p *Plugin) loadScheduleID(glyphID string) string {
	store := p.Services().ATSStore()
	if store == nil {
		return ""
	}

	subject := fmt.Sprintf("ix-json-glyph-%s", glyphID)
	attestations, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{subject},
		Predicates: []string{"scheduled"},
		Limit:      1,
	})
	if err != nil || len(attestations) == 0 {
		return ""
	}

	id, _ := attestations[0].Attributes["schedule_id"].(string)
	return id
}

// saveScheduleID persists the schedule ID in a separate "scheduled" attestation.
// Uses predicate "scheduled" (not "configured") to avoid clobbering glyph config.
func (p *Plugin) saveScheduleID(glyphID, scheduleID string) {
	store := p.Services().ATSStore()
	if store == nil {
		return
	}

	subject := fmt.Sprintf("ix-json-glyph-%s", glyphID)
	cmd := &atstypes.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{"scheduled"},
		Contexts:   []string{"_"},
		Attributes: map[string]any{"schedule_id": scheduleID},
		Source:     "ix-json",
	}

	if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
		p.Services().Logger("ix-json").Warnw("Failed to persist schedule ID", "glyph_id", glyphID, "error", err)
	}
}

// pauseSchedule pauses the Pulse schedule for a glyph.
func (p *Plugin) pauseSchedule(glyphID string) error {
	p.glyphMu.RLock()
	scheduleID, exists := p.glyphSchedules[glyphID]
	p.glyphMu.RUnlock()

	if !exists {
		return nil // No schedule to pause
	}

	schedSvc := p.Services().Schedule()
	if schedSvc == nil {
		return fmt.Errorf("ScheduleService not available")
	}

	if err := schedSvc.Pause(scheduleID); err != nil {
		return fmt.Errorf("failed to pause schedule %s for glyph %s: %w", scheduleID, glyphID, err)
	}

	p.Services().Logger("ix-json").Infow("Schedule paused", "glyph_id", glyphID, "schedule_id", scheduleID)
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

		pollRes, err := p.pollGlyph(ctx, req.GlyphID)
		if err != nil {
			return nil, nil, err
		}

		result, _ := json.Marshal(map[string]string{
			"status":   "poll completed",
			"glyph_id": req.GlyphID,
		})
		logs := []*protocol.JobLogEntry{
			protocol.NewJobLogEntry("info", "poll", fmt.Sprintf("Fetched %s, created %d attestations", pollRes.APIURL, pollRes.AttestationsCreated)),
		}
		return result, logs, nil

	default:
		return nil, nil, protocol.ErrUnknownHandler(handlerName)
	}
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
