package watcher

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"io"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// actionRequiresPlugin returns the plugin name this action depends on, or empty if none.
func actionRequiresPlugin(watcher *storage.Watcher) string {
	switch watcher.ActionType {
	case storage.ActionTypePython:
		return "python"
	case storage.ActionTypeElementExecute:
		var action ElementExecuteAction
		if err := json.Unmarshal([]byte(watcher.ActionData), &action); err == nil && action.TargetElementType == "py" {
			return "python"
		}
	case storage.ActionTypePluginExecute:
		var action PluginExecuteAction
		if err := json.Unmarshal([]byte(watcher.ActionData), &action); err == nil {
			return action.PluginName
		}
	}
	return ""
}

// executeAction executes a watcher's action with the triggering attestation
func (e *Engine) executeAction(watcher *storage.Watcher, as *types.As) {
	// If the action depends on a plugin that isn't loaded, queue it for later.
	// It will be picked up by drainLoop when the plugin is re-enabled.
	if required := actionRequiresPlugin(watcher); required != "" && e.pluginExecutor != nil {
		if !e.pluginExecutor.IsPluginLoaded(required) {
			e.enqueueAttestation(watcher.ID, as, "paused", 1, "plugin "+required+" not loaded")
			return
		}
	}

	var err error

	switch watcher.ActionType {
	case storage.ActionTypePython:
		err = e.executePython(watcher, as)
	case storage.ActionTypeWebhook:
		err = e.executeWebhook(watcher, as)
	case storage.ActionTypeElementExecute:
		err = e.executeElement(watcher, as)
	case storage.ActionTypePluginExecute:
		err = e.executePlugin(watcher, as)
	case storage.ActionTypeSemanticMatch:
		// Semantic match watchers only broadcast — no separate action to execute.
		// The match was already broadcast in OnAttestationCreated.
		return
	case storage.ActionTypeTell:
		// A tell runs nothing, which is what lets a node be born holding one.
		// Reaching here means something went around the match loop, and the
		// answer is to say so rather than to find something to run.
		e.logger.Errorw("A tell watcher reached executeAction and ran nothing",
			"watcher_id", watcher.ID, "attestation_id", as.ID)
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
		e.recordError(watcher.ID, err.Error(), as.ID)

		// Queue for retry via persistent queue
		e.enqueueAttestation(watcher.ID, as, "retry", 1, err.Error())
	} else {
		// Record success, and what caused it
		e.recordFire(watcher.ID, as.ID)

		// Update edge cursor for meld-edge watchers to prevent reprocessing on restart
		if watcher.ActionType == storage.ActionTypeElementExecute {
			e.updateEdgeCursor(watcher, as)
		}
	}
}

// applyEdgeCursor sets TimeStart on a meld-edge watcher's filter based on the stored cursor.
// This prevents reprocessing attestations that were already handled before a server restart.
func (e *Engine) applyEdgeCursor(w *storage.Watcher) {
	var action ElementExecuteAction
	if err := json.Unmarshal([]byte(w.ActionData), &action); err != nil {
		e.logger.Warnw("Cannot read action data to apply edge cursor; watcher will reprocess its history",
			"watcher_id", w.ID,
			"error", err)
		return
	}
	if action.CompositionID == "" {
		return
	}

	var lastProcessedAt time.Time
	err := e.db.QueryRowContext(e.ctx,
		"SELECT last_processed_at FROM composition_edge_cursors WHERE composition_id = ? AND from_element_id = ? AND to_element_id = ?",
		action.CompositionID, action.SourceElementID, action.TargetElementID,
	).Scan(&lastProcessedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return // No cursor yet — first run, process everything
	}
	if err != nil {
		// A failed read is not a first run: without the cursor this watcher
		// reprocesses everything it ever handled — duplicate executions, not
		// a clean start. That must not look like one.
		e.logger.Warnw("Failed to read edge cursor; watcher will reprocess its history",
			"watcher_id", w.ID,
			"composition_id", action.CompositionID,
			"error", err)
		return
	}

	// Set TimeStart to cursor timestamp so matchesFilter skips already-processed attestations
	w.Filter.TimeStart = &lastProcessedAt
}

// updateEdgeCursor records the last processed attestation for a meld-edge watcher.
// On server restart, loadWatchers applies the cursor as TimeStart to avoid reprocessing.
func (e *Engine) updateEdgeCursor(watcher *storage.Watcher, as *types.As) {
	var action ElementExecuteAction
	if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
		e.logger.Warnw("Cannot read action data to record edge cursor; watcher will reprocess this on restart",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
		return
	}
	if action.CompositionID == "" {
		return
	}

	_, err := e.db.ExecContext(e.ctx, `
		INSERT INTO composition_edge_cursors (composition_id, from_element_id, to_element_id, last_processed_id, last_processed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (composition_id, from_element_id, to_element_id)
		DO UPDATE SET last_processed_id = excluded.last_processed_id, last_processed_at = excluded.last_processed_at`,
		action.CompositionID, action.SourceElementID, action.TargetElementID, as.ID, as.Timestamp)
	if err != nil {
		e.logger.Warnw("Failed to update edge cursor",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
	}
}

// executePython executes Python code with the attestation injected
func (e *Engine) executePython(watcher *storage.Watcher, as *types.As) error {
	if e.pythonExecutor == nil {
		return errors.New("no python_provider plugin loaded")
	}

	// Inject attestation as a variable in the Python code
	attestationJSON, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	injectedCode := fmt.Sprintf(`
import json
_attestation_json = %q
attestation = json.loads(_attestation_json)

# User code below
%s
`, string(attestationJSON), watcher.ActionData)

	_, err = e.pythonExecutor.Execute(e.ctx, injectedCode, "", attestationJSON)
	return err
}

// ElementExecuteAction is the JSON structure stored in ActionData for element_execute watchers
type ElementExecuteAction struct {
	TargetElementID   string `json:"target_element_id"`
	TargetElementType string `json:"target_element_type"`
	CompositionID     string `json:"composition_id"`
	SourceElementID   string `json:"source_element_id"`
}

// executeElement executes a canvas element with the triggering attestation
func (e *Engine) executeElement(watcher *storage.Watcher, as *types.As) error {
	var action ElementExecuteAction
	if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
		return errors.Wrapf(err, "failed to parse element_execute action data for watcher %s", watcher.ID)
	}

	// Broadcast started
	if e.broadcastElementFired != nil {
		e.broadcastElementFired(action.TargetElementID, as.ID, "started", nil, nil)
	}

	// Fetch element's current content from canvas_elements
	var content sql.NullString
	err := e.db.QueryRowContext(e.ctx,
		"SELECT content FROM canvas_elements WHERE id = ?", action.TargetElementID,
	).Scan(&content)
	if err != nil {
		if e.broadcastElementFired != nil {
			e.broadcastElementFired(action.TargetElementID, as.ID, "error", err, nil)
		}
		return errors.Wrapf(err, "failed to fetch element %s content", action.TargetElementID)
	}

	attestationJSON, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	var execErr error
	var resultBody []byte
	switch action.TargetElementType {
	case "py":
		resultBody, execErr = e.executeElementPython(action.TargetElementID, content.String, attestationJSON)
	case "prompt":
		resultBody, execErr = e.executeElementPrompt(action.TargetElementID, content.String, attestationJSON)
	default:
		execErr = errors.Newf("unsupported element type for execution: %s (element %s)", action.TargetElementType, action.TargetElementID)
	}

	if e.broadcastElementFired != nil {
		if execErr != nil {
			e.broadcastElementFired(action.TargetElementID, as.ID, "error", execErr, nil)
		} else {
			e.broadcastElementFired(action.TargetElementID, as.ID, "success", nil, resultBody)
		}
	}

	return execErr
}

// executeElementPython runs a py element's content with the attestation injected as `upstream`.
// Returns the JSON-encoded execution result on success.
func (e *Engine) executeElementPython(elementID string, content string, attestationJSON []byte) ([]byte, error) {
	if e.pythonExecutor == nil {
		return nil, errors.New("no python_provider plugin loaded")
	}
	return e.pythonExecutor.Execute(e.ctx, content, elementID, attestationJSON)
}

// executeElementPrompt runs a prompt element's template with attestation fields interpolated.
// Returns the JSON-encoded execution result on success.
func (e *Engine) executeElementPrompt(elementID string, template string, attestationJSON []byte) (_ []byte, err error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"template":             template,
		"element_id":           elementID,
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
		return nil, errors.Wrapf(err, "failed to execute prompt element %s", elementID)
	}
	defer func() { err = sqlclose.With(err, resp.Body.Close(), "the prompt response body") }()

	body, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("prompt element %s execution failed (status %d): %s", elementID, resp.StatusCode, string(body))
	}
	// On 200 the body is the element's result. A short read here would hand back
	// half an answer as if it were the whole one.
	if readErr != nil {
		return nil, errors.Wrapf(readErr, "failed to read prompt element %s result", elementID)
	}

	return body, nil
}

// PluginExecuteAction is the JSON structure stored in ActionData for plugin_execute watchers
type PluginExecuteAction struct {
	PluginName  string `json:"plugin_name"`
	HandlerName string `json:"handler_name"`
}

// executePlugin dispatches a job to a named plugin via gRPC ExecuteJob
func (e *Engine) executePlugin(watcher *storage.Watcher, as *types.As) error {
	if e.pluginExecutor == nil {
		return errors.New("plugin executor not configured")
	}

	var action PluginExecuteAction
	if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
		return errors.Wrapf(err, "failed to parse plugin_execute action data for watcher %s", watcher.ID)
	}
	if action.PluginName == "" {
		return errors.Newf("plugin_execute action for watcher %s has empty plugin_name", watcher.ID)
	}
	if action.HandlerName == "" {
		return errors.Newf("plugin_execute action for watcher %s has empty handler_name", watcher.ID)
	}

	payload, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation for plugin execution")
	}

	_, err = e.pluginExecutor.ExecutePluginJob(e.ctx, action.PluginName, action.HandlerName, payload)
	if err != nil {
		return errors.Wrapf(err, "plugin %s handler %s failed for watcher %s", action.PluginName, action.HandlerName, watcher.ID)
	}

	return nil
}

// executeWebhook sends the attestation to a webhook URL
func (e *Engine) executeWebhook(watcher *storage.Watcher, as *types.As) (err error) {
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
	defer func() { err = sqlclose.With(err, resp.Body.Close(), "the webhook response body") }()

	if resp.StatusCode >= 400 {
		// An unread body leaves the status with nothing to explain it, which is
		// the whole reason the webhook refused.
		body, readErr := io.ReadAll(resp.Body)
		said := string(body)
		if readErr != nil {
			said += " (the rest of the body could not be read: " + readErr.Error() + ")"
		}
		return errors.Newf("webhook returned status %d: %s", resp.StatusCode, said)
	}

	return nil
}
