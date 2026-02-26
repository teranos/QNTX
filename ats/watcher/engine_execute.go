package watcher

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// executeAction executes a watcher's action with the triggering attestation
func (e *Engine) executeAction(watcher *storage.Watcher, as *types.As) {
	e.logger.Infow("Executing watcher action",
		"watcher_id", watcher.ID,
		"action_type", watcher.ActionType,
		"attestation_id", as.ID)

	var err error

	switch watcher.ActionType {
	case storage.ActionTypePython:
		err = e.executePython(watcher, as)
	case storage.ActionTypeWebhook:
		err = e.executeWebhook(watcher, as)
	case storage.ActionTypeGlyphExecute:
		err = e.executeGlyph(watcher, as)
	case storage.ActionTypeSemanticMatch:
		// Semantic match watchers only broadcast — no separate action to execute.
		// The match was already broadcast in OnAttestationCreated.
		return
	default:
		err = errors.Newf("unknown action type: %s", watcher.ActionType)
	}

	if err != nil {
		e.logger.Errorw("Watcher action failed",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)

		// Record error
		e.recordError(watcher.ID, err.Error())

		// Queue for retry via persistent queue
		e.enqueueAttestation(watcher.ID, as, "retry", 1, err.Error())
	} else {
		e.logger.Infow("Watcher action succeeded",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID)

		// Record success
		e.recordFire(watcher.ID)

		// Update edge cursor for meld-edge watchers to prevent reprocessing on restart
		if watcher.ActionType == storage.ActionTypeGlyphExecute {
			e.updateEdgeCursor(watcher, as)
		}
	}
}

// applyEdgeCursor sets TimeStart on a meld-edge watcher's filter based on the stored cursor.
// This prevents reprocessing attestations that were already handled before a server restart.
func (e *Engine) applyEdgeCursor(w *storage.Watcher) {
	var action GlyphExecuteAction
	if err := json.Unmarshal([]byte(w.ActionData), &action); err != nil || action.CompositionID == "" {
		return
	}

	var lastProcessedAt time.Time
	err := e.db.QueryRowContext(e.ctx,
		"SELECT last_processed_at FROM composition_edge_cursors WHERE composition_id = ? AND from_glyph_id = ? AND to_glyph_id = ?",
		action.CompositionID, action.SourceGlyphID, action.TargetGlyphID,
	).Scan(&lastProcessedAt)
	if err != nil {
		return // No cursor yet — first run, process everything
	}

	// Set TimeStart to cursor timestamp so matchesFilter skips already-processed attestations
	w.Filter.TimeStart = &lastProcessedAt
}

// updateEdgeCursor records the last processed attestation for a meld-edge watcher.
// On server restart, loadWatchers applies the cursor as TimeStart to avoid reprocessing.
func (e *Engine) updateEdgeCursor(watcher *storage.Watcher, as *types.As) {
	var action GlyphExecuteAction
	if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
		return
	}
	if action.CompositionID == "" {
		return
	}

	_, err := e.db.ExecContext(e.ctx, `
		INSERT INTO composition_edge_cursors (composition_id, from_glyph_id, to_glyph_id, last_processed_id, last_processed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (composition_id, from_glyph_id, to_glyph_id)
		DO UPDATE SET last_processed_id = excluded.last_processed_id, last_processed_at = excluded.last_processed_at`,
		action.CompositionID, action.SourceGlyphID, action.TargetGlyphID, as.ID, as.Timestamp)
	if err != nil {
		e.logger.Warnw("Failed to update edge cursor",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
	}
}

// executePython executes Python code with the attestation injected
func (e *Engine) executePython(watcher *storage.Watcher, as *types.As) error {
	// Inject attestation as a variable in the Python code
	attestationJSON, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	// Prepend code to inject the attestation
	injectedCode := fmt.Sprintf(`
import json
_attestation_json = %q
attestation = json.loads(_attestation_json)

# User code below
%s
`, string(attestationJSON), watcher.ActionData)

	// Call Python plugin
	reqBody, err := json.Marshal(map[string]interface{}{
		"content": injectedCode,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal request body")
	}

	url := e.apiBaseURL + "/api/python/execute"
	req, err := http.NewRequestWithContext(e.ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "failed to execute Python")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return errors.Newf("Python execution failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GlyphExecuteAction is the JSON structure stored in ActionData for glyph_execute watchers
type GlyphExecuteAction struct {
	TargetGlyphID   string `json:"target_glyph_id"`
	TargetGlyphType string `json:"target_glyph_type"`
	CompositionID   string `json:"composition_id"`
	SourceGlyphID   string `json:"source_glyph_id"`
}

// executeGlyph executes a canvas glyph with the triggering attestation
func (e *Engine) executeGlyph(watcher *storage.Watcher, as *types.As) error {
	var action GlyphExecuteAction
	if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
		return errors.Wrapf(err, "failed to parse glyph_execute action data for watcher %s", watcher.ID)
	}

	// Broadcast started
	if e.broadcastGlyphFired != nil {
		e.broadcastGlyphFired(action.TargetGlyphID, as.ID, "started", nil, nil)
	}

	// Fetch glyph's current content from canvas_glyphs
	var content sql.NullString
	err := e.db.QueryRowContext(e.ctx,
		"SELECT content FROM canvas_glyphs WHERE id = ?", action.TargetGlyphID,
	).Scan(&content)
	if err != nil {
		if e.broadcastGlyphFired != nil {
			e.broadcastGlyphFired(action.TargetGlyphID, as.ID, "error", err, nil)
		}
		return errors.Wrapf(err, "failed to fetch glyph %s content", action.TargetGlyphID)
	}

	attestationJSON, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	var execErr error
	var resultBody []byte
	switch action.TargetGlyphType {
	case "py":
		resultBody, execErr = e.executeGlyphPython(action.TargetGlyphID, content.String, attestationJSON)
	case "prompt":
		resultBody, execErr = e.executeGlyphPrompt(action.TargetGlyphID, content.String, attestationJSON)
	default:
		execErr = errors.Newf("unsupported glyph type for execution: %s (glyph %s)", action.TargetGlyphType, action.TargetGlyphID)
	}

	if e.broadcastGlyphFired != nil {
		if execErr != nil {
			e.broadcastGlyphFired(action.TargetGlyphID, as.ID, "error", execErr, nil)
		} else {
			e.broadcastGlyphFired(action.TargetGlyphID, as.ID, "success", nil, resultBody)
		}
	}

	return execErr
}

// executeGlyphPython runs a py glyph's content with the attestation injected as `upstream`.
// Returns the JSON-encoded execution result on success.
func (e *Engine) executeGlyphPython(glyphID string, content string, attestationJSON []byte) ([]byte, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"content":              content,
		"glyph_id":             glyphID,
		"upstream_attestation": json.RawMessage(attestationJSON),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal request body")
	}

	url := e.apiBaseURL + "/api/python/execute"
	req, err := http.NewRequestWithContext(e.ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute py glyph %s", glyphID)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("py glyph %s execution failed (status %d): %s", glyphID, resp.StatusCode, string(body))
	}

	return body, nil
}

// executeGlyphPrompt runs a prompt glyph's template with attestation fields interpolated.
// Returns the JSON-encoded execution result on success.
func (e *Engine) executeGlyphPrompt(glyphID string, template string, attestationJSON []byte) ([]byte, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"template":             template,
		"glyph_id":             glyphID,
		"upstream_attestation": json.RawMessage(attestationJSON),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal request body")
	}

	url := e.apiBaseURL + "/api/prompt/direct"
	req, err := http.NewRequestWithContext(e.ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute prompt glyph %s", glyphID)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("prompt glyph %s execution failed (status %d): %s", glyphID, resp.StatusCode, string(body))
	}

	return body, nil
}

// executeWebhook sends the attestation to a webhook URL
func (e *Engine) executeWebhook(watcher *storage.Watcher, as *types.As) error {
	body, err := json.Marshal(map[string]interface{}{
		"watcher_id":  watcher.ID,
		"attestation": as,
		"fired_at":    time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal webhook body")
	}

	req, err := http.NewRequestWithContext(e.ctx, "POST", watcher.ActionData, bytes.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "failed to create webhook request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "webhook request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return errors.Newf("webhook returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
