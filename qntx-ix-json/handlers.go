package qntxixjson

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

	// Glyph UI
	mux.HandleFunc("GET /ix-glyph", p.handleIXGlyph)
	mux.HandleFunc("GET /ix-glyph.css", p.handleIXGlyphCSS)

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
//   - Extract inline HTML/CSS/JS into separate files (//go:embed)
//   - Mapping must be editable from the UI, not just heuristics
//   - State changes (save, fetch, activate) should update inline without page reload
//   - Reuse existing glyph/panel design components instead of custom inline HTML
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

// handleIXGlyph renders the ix-json glyph UI.
func (p *Plugin) handleIXGlyph(w http.ResponseWriter, r *http.Request) {
	glyphID := r.URL.Query().Get("glyph_id")

	// Load config for THIS specific glyph instance
	glyphConfig := p.loadGlyphConfig(r.Context(), glyphID)

	p.glyphMu.RLock()
	mappingConfig := p.glyphMappings[glyphID]
	lastResponse := p.glyphResponses[glyphID]
	_, hasSchedule := p.glyphSchedules[glyphID]
	p.glyphMu.RUnlock()

	// Extract config from glyph-specific attestation
	apiURL := ""
	pollInterval := 0
	mode := ModePaused
	if hasSchedule {
		mode = ModeActiveRunning
	}

	if glyphConfig != nil {
		if url, ok := glyphConfig["api_url"].(string); ok {
			apiURL = url
		}
		if interval, ok := glyphConfig["poll_interval_seconds"].(float64); ok {
			pollInterval = int(interval)
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	version := p.Metadata().Version
	html := renderIXGlyphHTML(glyphID, mode, apiURL, pollInterval, mappingConfig, lastResponse, version)
	w.Write([]byte(html))
}

// renderIXGlyphHTML generates the HTML for the ix-json glyph UI.
func renderIXGlyphHTML(glyphID string, mode OperationMode, apiURL string, pollInterval int, mapping *MappingConfig, lastResponse []byte, version string) string {
	var html strings.Builder

	html.WriteString(fmt.Sprintf(`<div class="ix-json-content" data-glyph-id="%s">`, escapeHTMLAttr(glyphID)))

	// Header
	html.WriteString(`<div class="ix-header">`)
	html.WriteString(`<div class="ix-title">JSON API Ingestor</div>`)
	html.WriteString(fmt.Sprintf(`<div class="ix-mode mode-%s">%s</div>`, mode, mode))
	html.WriteString(fmt.Sprintf(`<div class="ix-version" style="font-size: 10px; color: #666; margin-left: auto;">v%s</div>`, escapeHTML(version)))
	html.WriteString(`</div>`)

	// Configuration section
	html.WriteString(`<div class="ix-section">`)
	html.WriteString(`<h3>Configuration</h3>`)
	html.WriteString(`<div class="config-form">`)
	html.WriteString(`<div class="form-group">`)
	html.WriteString(`<label>API URL</label>`)
	html.WriteString(fmt.Sprintf(`<input type="text" class="form-input ix-api-url" value="%s" placeholder="https://api.example.com/data">`, escapeHTMLAttr(apiURL)))
	html.WriteString(`</div>`)
	html.WriteString(`<div class="form-group">`)
	html.WriteString(`<label>Auth Token (optional)</label>`)
	html.WriteString(`<input type="password" class="form-input ix-auth-token" placeholder="Bearer token">`)
	html.WriteString(`</div>`)
	html.WriteString(`<div class="form-group">`)
	html.WriteString(`<label>Poll Interval (seconds, 0 = manual only)</label>`)
	html.WriteString(fmt.Sprintf(`<input type="number" class="form-input ix-poll-interval" value="%d" min="0" placeholder="300">`, pollInterval))
	html.WriteString(`</div>`)
	html.WriteString(`<div class="form-actions">`)
	html.WriteString(`<button class="ix-btn" onclick="ixSaveConfig(this)">Save Config</button>`)
	html.WriteString(`<button class="ix-btn" onclick="ixTestFetch(this)">Test Fetch</button>`)
	html.WriteString(`</div>`)
	html.WriteString(`<div class="ix-status"></div>`)
	html.WriteString(`</div>`)
	html.WriteString(`</div>`)

	// Response preview
	if len(lastResponse) > 0 {
		html.WriteString(`<div class="ix-section">`)
		html.WriteString(`<h3>API Response Preview</h3>`)

		// Pretty-print JSON
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, lastResponse, "", "  "); err == nil {
			html.WriteString(fmt.Sprintf(`<pre class="json-preview">%s</pre>`, escapeHTML(prettyJSON.String())))
		} else {
			html.WriteString(fmt.Sprintf(`<pre class="json-preview">%s</pre>`, escapeHTML(string(lastResponse))))
		}
		html.WriteString(`</div>`)
	}

	// Mapping configuration
	html.WriteString(`<div class="ix-section">`)
	html.WriteString(`<h3>Attestation Mapping</h3>`)
	if mapping != nil {
		html.WriteString(fmt.Sprintf(`<div class="mapping-item"><strong>Subject Path:</strong> %s</div>`, escapeHTML(mapping.SubjectPath)))
		html.WriteString(fmt.Sprintf(`<div class="mapping-item"><strong>Predicate Path:</strong> %s</div>`, escapeHTML(mapping.PredicatePath)))
		html.WriteString(fmt.Sprintf(`<div class="mapping-item"><strong>Context Path:</strong> %s</div>`, escapeHTML(mapping.ContextPath)))
		if len(mapping.RichFields) > 0 {
			html.WriteString(fmt.Sprintf(`<div class="mapping-item"><strong>Rich Fields:</strong> %s</div>`, escapeHTML(strings.Join(mapping.RichFields, ", "))))
		}
	} else {
		html.WriteString(`<div class="mapping-empty">No mapping configured. Run "Test Fetch" to infer a default mapping.</div>`)
	}
	html.WriteString(`</div>`)

	// Mode controls
	html.WriteString(`<div class="ix-section">`)
	html.WriteString(`<h3>Mode Controls</h3>`)
	html.WriteString(`<div class="mode-controls">`)
	html.WriteString(`<button class="ix-btn" onclick="ixSetMode(this, 'paused')">Pause</button>`)
	html.WriteString(`<button class="ix-btn ix-btn-primary" onclick="ixSetMode(this, 'active-running')">Activate</button>`)
	html.WriteString(`</div>`)
	html.WriteString(`</div>`)

	// JavaScript for interactivity
	html.WriteString(`<script>
// Find the glyph container from any element inside it
function ixContainer(el) { return el.closest('.ix-json-content'); }

function ixStatus(container, msg, isError) {
	var s = container.querySelector('.ix-status');
	s.textContent = msg;
	s.style.color = isError ? '#ef4444' : '#22c55e';
	if (!isError) { setTimeout(function() { s.textContent = ''; }, 4000); }
}

window.ixSaveConfig = async function(btn) {
	var c = ixContainer(btn);
	var glyphId = c.dataset.glyphId;
	var apiUrl = c.querySelector('.ix-api-url').value;
	var authToken = c.querySelector('.ix-auth-token').value;
	var pollInterval = parseInt(c.querySelector('.ix-poll-interval').value) || 0;

	try {
		var res = await fetch('/api/ix-json/update-config', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				glyph_id: glyphId,
				api_url: apiUrl,
				auth_token: authToken,
				poll_interval_seconds: pollInterval
			})
		});
		if (res.ok) {
			ixStatus(c, 'Configuration saved', false);
		} else {
			var body = await res.json().catch(function() { return {}; });
			ixStatus(c, body.error || 'Save failed', true);
		}
	} catch (e) {
		ixStatus(c, e.message, true);
	}
};

window.ixTestFetch = async function(btn) {
	var c = ixContainer(btn);
	var glyphId = c.dataset.glyphId;
	var apiUrl = c.querySelector('.ix-api-url').value;
	var authToken = c.querySelector('.ix-auth-token').value;

	if (!apiUrl) { ixStatus(c, 'API URL is required', true); return; }

	ixStatus(c, 'Fetching...', false);
	try {
		var res = await fetch('/api/ix-json/test-fetch', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ glyph_id: glyphId, api_url: apiUrl, auth_token: authToken })
		});
		if (res.ok) {
			ixStatus(c, 'Fetch successful', false);
		} else {
			var body = await res.json().catch(function() { return {}; });
			ixStatus(c, body.error || 'Fetch failed', true);
		}
	} catch (e) {
		ixStatus(c, e.message, true);
	}
};

window.ixSetMode = async function(btn, mode) {
	var c = ixContainer(btn);
	var glyphId = c.dataset.glyphId;
	var apiUrl = c.querySelector('.ix-api-url').value;
	var authToken = c.querySelector('.ix-auth-token').value;
	var pollInterval = parseInt(c.querySelector('.ix-poll-interval').value) || 0;
	try {
		var res = await fetch('/api/ix-json/set-mode', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				glyph_id: glyphId,
				mode: mode,
				api_url: apiUrl,
				auth_token: authToken,
				poll_interval_seconds: pollInterval
			})
		});
		var body = await res.json().catch(function() { return {}; });
		if (res.ok) {
			ixStatus(c, body.status || 'Mode: ' + mode, false);
		} else {
			ixStatus(c, body.error || 'Failed to set mode', true);
		}
	} catch (e) {
		ixStatus(c, e.message, true);
	}
};
</script>`)

	html.WriteString(`</div>`) // end ix-json-content
	return html.String()
}

// handleIXGlyphCSS returns the CSS stylesheet for the ix-json glyph.
func (p *Plugin) handleIXGlyphCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")

	css := `
.ix-json-content {
	display: flex;
	flex-direction: column;
	height: 100%;
	background: var(--background, #fff);
	color: var(--foreground, #000);
	padding: 8px;
	overflow-y: auto;
}

.ix-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	padding-bottom: 6px;
	border-bottom: 1px solid var(--border-color, #e0e0e0);
	margin-bottom: 8px;
}

.ix-title {
	font-size: 13px;
	font-weight: 600;
}

.ix-mode {
	padding: 2px 6px;
	border-radius: 3px;
	font-size: 11px;
	font-weight: 500;
	text-transform: uppercase;
}

.mode-paused {
	background: #fee2e2;
	color: #991b1b;
}

.mode-active-running {
	background: #d1fae5;
	color: #065f46;
}

.ix-section {
	margin-bottom: 8px;
}

.ix-section h3 {
	font-size: 11px;
	font-weight: 600;
	margin-bottom: 4px;
	color: var(--muted-foreground, #666);
	text-transform: uppercase;
	letter-spacing: 0.5px;
}

.config-item, .mapping-item {
	padding: 2px 0;
	font-size: 12px;
}

.config-item strong, .mapping-item strong {
	color: var(--foreground, #000);
	margin-right: 4px;
}

.config-form {
	display: flex;
	flex-direction: column;
	gap: 4px;
}

.form-group {
	display: flex;
	flex-direction: column;
	gap: 1px;
}

.form-group label {
	font-size: 11px;
	font-weight: 500;
	color: var(--muted-foreground, #666);
}

.form-input {
	padding: 3px 6px;
	border: 1px solid var(--border-color, #ccc);
	border-radius: 3px;
	font-size: 12px;
	background: var(--background, #fff);
	color: var(--foreground, #000);
}

.form-input:focus {
	outline: none;
	border-color: var(--primary, #0066cc);
}

.form-actions {
	display: flex;
	gap: 4px;
	margin-top: 2px;
}

.json-preview {
	background: var(--card-bg, #f9f9f9);
	border: 1px solid var(--border-color, #e0e0e0);
	border-radius: 3px;
	padding: 6px;
	font-size: 11px;
	font-family: monospace;
	overflow-x: auto;
	max-height: 200px;
	overflow-y: auto;
}

.mapping-empty {
	padding: 6px;
	background: var(--card-bg, #f9f9f9);
	border-radius: 3px;
	color: var(--muted-foreground, #666);
	font-size: 12px;
}

.mode-controls {
	display: flex;
	gap: 4px;
}

.ix-btn {
	padding: 3px 8px;
	background: var(--card-bg, #f0f0f0);
	border: 1px solid var(--border-color, #ccc);
	border-radius: 3px;
	cursor: pointer;
	font-size: 12px;
	font-weight: 500;
}

.ix-btn:hover {
	background: var(--hover-bg, #e0e0e0);
}

.ix-btn-primary {
	background: var(--primary, #0066cc);
	color: #fff;
	border-color: var(--primary, #0066cc);
}

.ix-btn-primary:hover {
	background: var(--primary-hover, #0052a3);
}

.ix-status {
	font-size: 11px;
	font-family: monospace;
	min-height: 14px;
	margin-top: 2px;
}
`

	w.Write([]byte(css))
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

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func escapeHTMLAttr(s string) string {
	return escapeHTML(s)
}
