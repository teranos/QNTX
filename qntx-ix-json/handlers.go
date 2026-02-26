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
		p.services.Logger("ix-json").Errorw("Test fetch failed", "api_url", req.APIURL, "error", err)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch from API: %v", err))
		return
	}

	// Cache response and infer mapping per-glyph
	p.mu.Lock()
	if req.GlyphID != "" {
		p.glyphResponses[req.GlyphID] = data
		if p.glyphMappings[req.GlyphID] == nil {
			p.glyphMappings[req.GlyphID] = p.inferMapping(data)
		}
	}
	mapping := p.glyphMappings[req.GlyphID]
	p.mu.Unlock()

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

	store := p.services.ATSStore()
	if store == nil {
		writeError(w, http.StatusInternalServerError, "ATSStore not available")
		return
	}

	// Create attestation for THIS glyph instance
	// Subject: ix-json-glyph-{glyphID}
	subject := fmt.Sprintf("ix-json-glyph-%s", req.GlyphID)

	attrs := map[string]interface{}{
		"api_url":               req.APIURL,
		"poll_interval_seconds": req.PollIntervalSeconds,
	}
	if req.AuthToken != "" {
		attrs["auth_token"] = req.AuthToken
	}

	cmd := &atstypes.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Contexts:   []string{"_"},
		Attributes: attrs,
		Source:     "ix-json-ui",
	}

	if _, err := store.GenerateAndCreateAttestation(r.Context(), cmd); err != nil {
		p.services.Logger("ix-json").Errorw("Failed to create glyph configuration attestation",
			"glyph_id", req.GlyphID,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save configuration: %v", err))
		return
	}

	p.services.Logger("ix-json").Infow("Glyph configuration attested",
		"glyph_id", req.GlyphID,
		"api_url", req.APIURL,
		"has_auth_token", req.AuthToken != "",
		"poll_interval_seconds", req.PollIntervalSeconds,
	)

	writeJSON(w, http.StatusOK, map[string]string{
		"status": fmt.Sprintf("Configuration saved for glyph %s ✓", req.GlyphID),
	})
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

	p.mu.Lock()
	if req.GlyphID != "" {
		p.glyphMappings[req.GlyphID] = mapping
	}
	p.mu.Unlock()

	p.services.Logger("ix-json").Infow("Mapping configuration updated", "glyph_id", req.GlyphID, "mapping", mapping)
	writeJSON(w, http.StatusOK, map[string]string{"status": "Mapping updated"})
}

// handleSetMode changes the plugin operation mode.
func (p *Plugin) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode OperationMode `json:"mode"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	p.mu.Lock()
	p.mode = req.Mode
	p.mu.Unlock()

	p.services.Logger("ix-json").Infow("Mode changed", "mode", req.Mode)
	writeJSON(w, http.StatusOK, map[string]string{"status": fmt.Sprintf("Mode set to %s", req.Mode)})
}

// handleStatus returns the current plugin status.
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	status := map[string]interface{}{
		"mode":    p.mode,
		"api_url": p.apiURL,
	}
	p.mu.RUnlock()

	writeJSON(w, http.StatusOK, status)
}

// handleIXGlyph renders the ix-json glyph UI.
func (p *Plugin) handleIXGlyph(w http.ResponseWriter, r *http.Request) {
	glyphID := r.URL.Query().Get("glyph_id")

	// Load config for THIS specific glyph instance
	glyphConfig := p.loadGlyphConfig(r.Context(), glyphID)

	p.mu.RLock()
	mappingConfig := p.glyphMappings[glyphID]
	lastResponse := p.glyphResponses[glyphID]
	p.mu.RUnlock()

	// Extract config from glyph-specific attestation
	apiURL := ""
	pollInterval := 0
	mode := ModeDataShaping

	if glyphConfig != nil {
		if url, ok := glyphConfig["api_url"].(string); ok {
			apiURL = url
		}
		if interval, ok := glyphConfig["poll_interval_seconds"].(float64); ok {
			pollInterval = int(interval)
			if pollInterval > 0 {
				mode = ModeActiveRunning
			}
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
	html.WriteString(`<div class="ix-title">🔄 JSON API Ingestor</div>`)
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
	html.WriteString(`<button class="ix-btn" onclick="ixSetMode(this, 'data-shaping')">Data Shaping</button>`)
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
	try {
		var res = await fetch('/api/ix-json/set-mode', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ mode: mode })
		});
		if (res.ok) {
			ixStatus(c, 'Mode: ' + mode, false);
		} else {
			var body = await res.json().catch(function() { return {}; });
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
	padding: 16px;
	overflow-y: auto;
}

.ix-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	padding-bottom: 16px;
	border-bottom: 2px solid var(--border-color, #e0e0e0);
	margin-bottom: 16px;
}

.ix-title {
	font-size: 18px;
	font-weight: 600;
}

.ix-mode {
	padding: 4px 12px;
	border-radius: 4px;
	font-size: 12px;
	font-weight: 500;
	text-transform: uppercase;
}

.mode-data-shaping {
	background: #fef3c7;
	color: #92400e;
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
	margin-bottom: 24px;
}

.ix-section h3 {
	font-size: 14px;
	font-weight: 600;
	margin-bottom: 12px;
	color: var(--muted-foreground, #666);
}

.config-item, .mapping-item {
	padding: 8px 0;
	font-size: 14px;
}

.config-item strong, .mapping-item strong {
	color: var(--foreground, #000);
	margin-right: 8px;
}

.config-form {
	display: flex;
	flex-direction: column;
	gap: 12px;
}

.form-group {
	display: flex;
	flex-direction: column;
	gap: 4px;
}

.form-group label {
	font-size: 12px;
	font-weight: 600;
	color: var(--muted-foreground, #666);
}

.form-input {
	padding: 8px 12px;
	border: 1px solid var(--border-color, #ccc);
	border-radius: 4px;
	font-size: 14px;
	background: var(--background, #fff);
	color: var(--foreground, #000);
}

.form-input:focus {
	outline: none;
	border-color: var(--primary, #0066cc);
}

.form-actions {
	display: flex;
	gap: 8px;
	margin-top: 8px;
}

.json-preview {
	background: var(--card-bg, #f9f9f9);
	border: 1px solid var(--border-color, #e0e0e0);
	border-radius: 4px;
	padding: 12px;
	font-size: 12px;
	font-family: monospace;
	overflow-x: auto;
	max-height: 300px;
	overflow-y: auto;
}

.mapping-empty {
	padding: 16px;
	background: var(--card-bg, #f9f9f9);
	border-radius: 4px;
	color: var(--muted-foreground, #666);
	font-size: 14px;
}

.mode-controls {
	display: flex;
	gap: 8px;
}

.ix-btn {
	padding: 8px 16px;
	background: var(--card-bg, #f0f0f0);
	border: 1px solid var(--border-color, #ccc);
	border-radius: 4px;
	cursor: pointer;
	font-size: 14px;
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
	font-size: 12px;
	font-family: monospace;
	min-height: 18px;
	margin-top: 4px;
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
