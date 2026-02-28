package qntxixjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	atstypes "github.com/teranos/QNTX/ats/types"
)

// registerHTTPHandlers registers all HTTP handlers for the ix-json plugin.
func (p *Plugin) registerHTTPHandlers(mux *http.ServeMux) error {
	// IMPORTANT: Register routes WITHOUT /api/ix-json prefix
	// The server strips the prefix automatically before routing to this mux
	//
	// Example flow:
	//   Browser requests: /api/ix-json/update-config
	//   Server strips:    /api/ix-json
	//   Plugin receives:  /update-config
	//
	// Therefore, register as "/update-config" NOT "/api/ix-json/update-config"

	// Glyph UI (SDK module — replaces legacy HTML pipeline)
	mux.HandleFunc("GET /ix-glyph-module.js", p.handleIXGlyphModule)

	// API operations
	mux.HandleFunc("POST /test-fetch", p.handleTestFetch)
	mux.HandleFunc("POST /update-config", p.handleUpdateConfig)
	mux.HandleFunc("POST /update-mapping", p.handleUpdateMapping)
	mux.HandleFunc("POST /set-mode", p.handleSetMode)
	mux.HandleFunc("GET /status", p.handleStatus)

	return nil
}

// handleTestFetch triggers a single API fetch for data exploration.
func (p *Plugin) handleTestFetch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GlyphID   string `json:"glyph_id"`
		APIURL    string `json:"api_url"`
		AuthToken string `json:"auth_token"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.APIURL == "" {
		writeError(w, http.StatusBadRequest, "API URL not configured")
		return
	}

	data, err := p.fetchJSON(r.Context(), req.APIURL, req.AuthToken)
	if err != nil {
		p.Services().Logger("ix-json").Errorw("Test fetch failed", "api_url", req.APIURL, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch from API: %v", err))
		return
	}

	// Cache response and infer mapping per-glyph
	p.glyphMu.Lock()
	if req.GlyphID != "" {
		p.glyphResponses[req.GlyphID] = data
		if p.glyphMappings[req.GlyphID] == nil {
			p.glyphMappings[req.GlyphID] = p.inferMapping(data)
		}
	}
	mapping := p.glyphMappings[req.GlyphID]
	p.glyphMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":    json.RawMessage(data),
		"mapping": mapping,
	})
}

// handleUpdateConfig updates the configuration by creating a glyph-specific attestation.
func (p *Plugin) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GlyphID             string `json:"glyph_id"`
		APIURL              string `json:"api_url"`
		AuthToken           string `json:"auth_token"`
		PollIntervalSeconds int    `json:"poll_interval_seconds"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.GlyphID == "" {
		writeError(w, http.StatusBadRequest, "glyph_id is required")
		return
	}

	// FIXME: saving config doesn't refresh the response preview — requires page reload to see persisted values
	if err := p.saveGlyphConfig(r.Context(), req.GlyphID, req.APIURL, req.AuthToken, req.PollIntervalSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save configuration: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "Configuration saved",
	})
}

// saveGlyphConfig persists glyph configuration as an attestation.
func (p *Plugin) saveGlyphConfig(ctx context.Context, glyphID, apiURL, authToken string, pollIntervalSecs int) error {
	store := p.Services().ATSStore()
	if store == nil {
		return fmt.Errorf("ATSStore not available")
	}

	subject := fmt.Sprintf("ix-json-glyph-%s", glyphID)
	attrs := map[string]any{
		"api_url":               apiURL,
		"poll_interval_seconds": pollIntervalSecs,
	}
	if authToken != "" {
		attrs["auth_token"] = authToken
	}

	cmd := &atstypes.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Contexts:   []string{"_"},
		Attributes: attrs,
		Source:     "ix-json-ui",
	}

	if _, err := store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
		return err
	}

	p.Services().Logger("ix-json").Infow("Glyph configuration attested",
		"glyph_id", glyphID,
		"api_url", apiURL,
		"poll_interval_seconds", pollIntervalSecs,
	)
	return nil
}

// handleUpdateMapping updates the mapping configuration.
func (p *Plugin) handleUpdateMapping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GlyphID       string            `json:"glyph_id"`
		SubjectPath   string            `json:"subject_path"`
		PredicatePath string            `json:"predicate_path"`
		ContextPath   string            `json:"context_path"`
		RichFields    []string          `json:"rich_fields"`
		KeyRemapping  map[string]string `json:"key_remapping"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	mapping := &MappingConfig{
		SubjectPath:   req.SubjectPath,
		PredicatePath: req.PredicatePath,
		ContextPath:   req.ContextPath,
		RichFields:    req.RichFields,
		KeyRemapping:  req.KeyRemapping,
	}

	p.glyphMu.Lock()
	if req.GlyphID != "" {
		p.glyphMappings[req.GlyphID] = mapping
	}
	p.glyphMu.Unlock()

	p.Services().Logger("ix-json").Infow("Mapping configuration updated", "glyph_id", req.GlyphID, "mapping", mapping)
	writeJSON(w, http.StatusOK, map[string]string{"status": "Mapping updated"})
}

// handleSetMode changes the operation mode for a specific glyph.
// On activate: saves current config to attestations, then starts poller.
func (p *Plugin) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GlyphID             string        `json:"glyph_id"`
		Mode                OperationMode `json:"mode"`
		APIURL              string        `json:"api_url"`
		AuthToken           string        `json:"auth_token"`
		PollIntervalSeconds int           `json:"poll_interval_seconds"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.GlyphID == "" {
		writeError(w, http.StatusBadRequest, "glyph_id is required")
		return
	}

	logger := p.Services().Logger("ix-json")

	switch req.Mode {
	case ModeActiveRunning:
		if req.PollIntervalSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "Poll interval must be > 0 to activate")
			return
		}

		// Check mapping exists
		p.glyphMu.RLock()
		mapping := p.glyphMappings[req.GlyphID]
		p.glyphMu.RUnlock()
		if mapping == nil {
			writeError(w, http.StatusBadRequest, "No mapping configured — fetch and configure mapping first")
			return
		}

		// Save config to attestations so it persists
		if err := p.saveGlyphConfig(r.Context(), req.GlyphID, req.APIURL, req.AuthToken, req.PollIntervalSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save config: %v", err))
			return
		}

		if err := p.createSchedule(req.GlyphID, req.PollIntervalSeconds); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create schedule: %v", err))
			return
		}
		logger.Infow("Glyph schedule activated", "glyph_id", req.GlyphID, "interval_seconds", req.PollIntervalSeconds)
		writeJSON(w, http.StatusOK, map[string]string{
			"status": fmt.Sprintf("Schedule active (every %ds)", req.PollIntervalSeconds),
		})

	case ModePaused:
		if err := p.pauseSchedule(req.GlyphID); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to pause schedule: %v", err))
			return
		}
		logger.Infow("Glyph schedule paused", "glyph_id", req.GlyphID)
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "Schedule paused",
		})

	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown mode: %s", req.Mode))
	}
}

// handleStatus returns the current plugin status.
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	p.glyphMu.RLock()
	activeSchedules := len(p.glyphSchedules)
	p.glyphMu.RUnlock()

	mode := ModeActiveRunning
	if p.IsPaused() {
		mode = ModePaused
	}
	status := map[string]any{
		"mode":             mode,
		"active_schedules": activeSchedules,
	}

	writeJSON(w, http.StatusOK, status)
}

// Known limitations — tracked as GitHub issues:
//
// #626 Glyph UI redesign:
//   - Mapping must be editable from the UI, not just heuristics
//   - Response preview should show one item at a time, not dump the full array
//   - Mapping config should be integrated into the preview (click field → assign SPC role)
//   - Mode badge and UI must reflect state changes (pause/activate) without reload
//
// #627 HTTP client capabilities:
//   - Only supports GET with Bearer token — no custom headers, query params, POST, other auth
//   - No redirect policy control, no pagination, no configurable timeout
//   - No rate-limiting — will hammer APIs if poll interval is too aggressive
//   - No meaningful error feedback — all failures surface as generic "fetch failed"
//
// #628 Data pipeline and meld integration:
//   - Glyph must be meldable — compose with py/se/ax glyphs for filtering/transform
//   - Watcher system integration is undefined
//   - ix-json is one specialization of the broader ix universal ingestor vision
//
// #629 Type attestation at ingestion time:
//   - Field type vocabulary: rich_string, unique (dedup key), secret (credential), array (tags)
//   - Should be attestable during mapping setup, not in a separate modal
//   - Related: #479 (embeddings), #311 (type field config UI), #291 (array field metadata)
//
// #630 Secrets and variables system:
//   - Auth tokens stored as plain attestation attributes — no encryption, rotation, or reuse

// handleIXGlyphModule serves the embedded glyph module.
func (p *Plugin) handleIXGlyphModule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(ixGlyphModuleJS)
}

// fetchJSON performs an HTTP GET to the API and returns the raw JSON response.
func (p *Plugin) fetchJSON(ctx context.Context, apiURL, authToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed for %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, nil
}

// Helper functions for HTTP responses
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return err
	}
	return nil
}

