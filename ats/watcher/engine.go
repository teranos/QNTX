package watcher

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Engine manages watchers and executes actions when attestations match filters
type Engine struct {
	store  *storage.WatcherStore
	logger *zap.SugaredLogger
	db     *sql.DB // Direct database access for querying historical attestations

	// Base URL for API calls (e.g., "http://localhost:877")
	apiBaseURL string

	// HTTP client with timeout for external calls
	httpClient *http.Client

	// Broadcast callback for watcher matches (optional)
	// Called when an attestation matches a watcher's filter
	broadcastMatch func(watcherID string, attestation *types.As)

	// Broadcast callback for glyph execution events (optional)
	// Called when a glyph_execute action fires, with status updates and execution result
	broadcastGlyphFired func(glyphID string, attestationID string, status string, err error, result []byte)

	// In-memory state
	mu           sync.RWMutex
	watchers     map[string]*storage.Watcher
	rateLimiters map[string]*rate.Limiter
	parseErrors  map[string]error // Stores parse errors for watchers that failed to load

	// Retry queue
	retryMu    sync.Mutex
	retryQueue []*PendingExecution

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// PendingExecution represents a failed execution queued for retry
type PendingExecution struct {
	WatcherID   string
	Attestation *types.As
	Attempt     int
	NextRetryAt time.Time
	LastError   string
}

const (
	maxRetries       = 5
	initialBackoff   = 1 * time.Second
	maxBackoff       = 60 * time.Second
	retryTickerInterval = 1 * time.Second
)

// NewEngine creates a new watcher engine
func NewEngine(db *sql.DB, apiBaseURL string, logger *zap.SugaredLogger) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		store:        storage.NewWatcherStore(db),
		logger:       logger,
		db:           db,
		apiBaseURL:   strings.TrimSuffix(apiBaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		watchers:     make(map[string]*storage.Watcher),
		rateLimiters: make(map[string]*rate.Limiter),
		parseErrors:  make(map[string]error),
		retryQueue:   make([]*PendingExecution, 0),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start loads watchers from DB and starts the retry loop
func (e *Engine) Start() error {
	if err := e.loadWatchers(); err != nil {
		return errors.Wrap(err, "failed to load watchers")
	}

	// Start retry loop
	e.wg.Add(1)
	go e.retryLoop()

	e.logger.Infow("Watcher engine started", "watchers_loaded", len(e.watchers))
	return nil
}

// Stop gracefully shuts down the engine
func (e *Engine) Stop() {
	e.cancel()
	e.wg.Wait()
	e.logger.Info("Watcher engine stopped")
}

// loadWatchers loads all enabled watchers from the database and parses AX queries
func (e *Engine) loadWatchers() error {
	watchers, err := e.store.List(e.ctx, true) // enabled only
	if err != nil {
		return errors.Wrap(err, "failed to list enabled watchers from store")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear stale in-memory state — watchers deleted from DB must not keep firing
	e.watchers = make(map[string]*storage.Watcher, len(watchers))
	e.rateLimiters = make(map[string]*rate.Limiter, len(watchers))
	e.parseErrors = make(map[string]error)

	for _, w := range watchers {
		// If watcher has an AX query string, parse it into the Filter
		if w.AxQuery != "" {
			filter, err := parser.ParseAxCommandWithContext(
				strings.Fields(w.AxQuery),
				0,
				parser.ErrorContextPlain,
			)
			if err != nil {
				// Wrap error with watcher context before storing
				enrichedErr := errors.Wrapf(err, "failed to parse AX query for watcher %s: %q", w.ID, w.AxQuery)
				e.logger.Warnw("Failed to parse AX query for watcher, skipping",
					"watcher_id", w.ID,
					"ax_query", w.AxQuery,
					"error", enrichedErr)
				// Store enriched error for retrieval
				e.parseErrors[w.ID] = enrichedErr
				continue
			}
			// Merge parsed filter into watcher's filter
			w.Filter = *filter
			// Clear any previous parse error for this watcher
			delete(e.parseErrors, w.ID)
		}

		// Apply edge cursor for meld-edge watchers: skip attestations already processed
		if w.ActionType == storage.ActionTypeGlyphExecute {
			e.applyEdgeCursor(w)
		}

		e.watchers[w.ID] = w
		// Create rate limiter: MaxFiresPerMinute/60 = fires per second
		// If MaxFiresPerMinute is 0, rate is 0/60 = 0, which means no fires allowed (QNTX LAW: zero means zero)
		e.rateLimiters[w.ID] = rate.NewLimiter(rate.Limit(float64(w.MaxFiresPerMinute)/60.0), 1)
	}

	return nil
}

// ReloadWatchers reloads watchers from the database (call after CRUD operations)
func (e *Engine) ReloadWatchers() error {
	return e.loadWatchers()
}

// SetBroadcastCallback sets the callback function for broadcasting watcher matches
func (e *Engine) SetBroadcastCallback(callback func(watcherID string, attestation *types.As)) {
	e.broadcastMatch = callback
}

// SetGlyphFiredCallback sets the callback for glyph execution notifications
func (e *Engine) SetGlyphFiredCallback(callback func(glyphID string, attestationID string, status string, err error, result []byte)) {
	e.broadcastGlyphFired = callback
}

// GetWatcher returns a watcher from the in-memory map if it exists
// Used to verify that a watcher was successfully loaded after parsing
func (e *Engine) GetWatcher(watcherID string) (*storage.Watcher, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	watcher, exists := e.watchers[watcherID]
	return watcher, exists
}

// GetParseError returns the parse error for a watcher that failed to load
// Returns nil if the watcher loaded successfully or doesn't exist
func (e *Engine) GetParseError(watcherID string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.parseErrors[watcherID]
}

// QueryHistoricalMatches queries all historical attestations and broadcasts matches for a watcher
// This is called when a watcher is created/updated to show existing matches, not just new ones
func (e *Engine) QueryHistoricalMatches(watcherID string) error {
	// Get watcher from in-memory map
	e.mu.RLock()
	watcher, exists := e.watchers[watcherID]
	e.mu.RUnlock()

	if !exists {
		return errors.Newf("watcher %s not found in engine", watcherID)
	}

	// Query all attestations from database
	query := `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes
	          FROM attestations
	          ORDER BY timestamp DESC`

	rows, err := e.db.Query(query)
	if err != nil {
		return errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	matchCount := 0
	for rows.Next() {
		var as types.As
		var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON, attributesJSON []byte

		err := rows.Scan(
			&as.ID,
			&subjectsJSON,
			&predicatesJSON,
			&contextsJSON,
			&actorsJSON,
			&as.Timestamp,
			&as.Source,
			&attributesJSON,
		)
		if err != nil {
			e.logger.Warnw("Failed to scan attestation row",
				"watcher_id", watcherID,
				"error", err)
			continue
		}

		// Parse JSON arrays
		if err := json.Unmarshal(subjectsJSON, &as.Subjects); err != nil {
			e.logger.Warnw("Failed to unmarshal subjects",
				"watcher_id", watcherID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}
		if err := json.Unmarshal(predicatesJSON, &as.Predicates); err != nil {
			e.logger.Warnw("Failed to unmarshal predicates",
				"watcher_id", watcherID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}
		if err := json.Unmarshal(contextsJSON, &as.Contexts); err != nil {
			e.logger.Warnw("Failed to unmarshal contexts",
				"watcher_id", watcherID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}
		if err := json.Unmarshal(actorsJSON, &as.Actors); err != nil {
			e.logger.Warnw("Failed to unmarshal actors",
				"watcher_id", watcherID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}
		if len(attributesJSON) > 0 && string(attributesJSON) != "null" {
			if err := json.Unmarshal(attributesJSON, &as.Attributes); err != nil {
				e.logger.Warnw("Failed to unmarshal attributes",
					"watcher_id", watcherID,
					"attestation_id", as.ID,
					"error", err)
				// Continue - attributes are optional
			}
		}

		// Check if attestation matches watcher filter
		if e.matchesFilter(&as, watcher) {
			matchCount++
			// Broadcast match using callback if set
			if e.broadcastMatch != nil {
				e.broadcastMatch(watcherID, &as)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "error iterating attestation rows")
	}

	e.logger.Infow("Historical query completed",
		"watcher_id", watcherID,
		"matches_found", matchCount)

	return nil
}

// OnAttestationCreated is called when a new attestation is created
// This is the main entry point for the watcher system
func (e *Engine) OnAttestationCreated(as *types.As) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, watcher := range e.watchers {
		if !watcher.Enabled {
			continue
		}

		if !e.matchesFilter(as, watcher) {
			continue
		}

		// Broadcast match to frontend (for live results display)
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcher.ID, as)
		}

		// Check rate limit for action execution
		// Per QNTX LAW: "Zero means zero" - if MaxFiresPerMinute is 0, never execute
		if watcher.MaxFiresPerMinute == 0 {
			e.logger.Infow("Watcher has MaxFiresPerMinute=0, not executing",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID)
			continue
		}
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.logger.Infow("Watcher rate limited",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID)
			continue
		}

		// Execute async with a deep copy to prevent race conditions
		// Each goroutine gets its own copy of the attestation
		asCopy := *as  // Copy the struct
		// Deep copy slices to prevent shared references
		asCopy.Subjects = append([]string(nil), as.Subjects...)
		asCopy.Predicates = append([]string(nil), as.Predicates...)
		asCopy.Contexts = append([]string(nil), as.Contexts...)
		asCopy.Actors = append([]string(nil), as.Actors...)
		if as.Attributes != nil {
			asCopy.Attributes = make(map[string]interface{})
			for k, v := range as.Attributes {
				asCopy.Attributes[k] = v
			}
		}
		go e.executeAction(watcher, &asCopy)
	}
}

// matchesFilter checks if an attestation matches a watcher's filter using exact field matching
func (e *Engine) matchesFilter(as *types.As, watcher *storage.Watcher) bool {
	filter := &watcher.Filter

	// Empty filter = match all
	if len(filter.Subjects) > 0 && !hasOverlap(filter.Subjects, as.Subjects) {
		return false
	}
	if len(filter.Predicates) > 0 && !hasOverlap(filter.Predicates, as.Predicates) {
		return false
	}
	if len(filter.Contexts) > 0 && !hasOverlap(filter.Contexts, as.Contexts) {
		return false
	}
	if len(filter.Actors) > 0 && !hasOverlap(filter.Actors, as.Actors) {
		return false
	}
	if filter.TimeStart != nil && as.Timestamp.Before(*filter.TimeStart) {
		return false
	}
	if filter.TimeEnd != nil && as.Timestamp.After(*filter.TimeEnd) {
		return false
	}
	return true
}

// hasOverlap returns true if there's any overlap between two string slices
func hasOverlap(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, v := range a {
		set[strings.ToLower(v)] = true
	}
	for _, v := range b {
		if set[strings.ToLower(v)] {
			return true
		}
	}
	return false
}

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
	default:
		err = errors.Newf("unknown action type: %s", watcher.ActionType)
	}

	if err != nil {
		e.logger.Errorw("Watcher action failed",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)

		// Record error
		e.store.RecordError(e.ctx, watcher.ID, err.Error())

		// Queue for retry
		e.queueRetry(watcher.ID, as, 1, err.Error())
	} else {
		e.logger.Infow("Watcher action succeeded",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID)

		// Record success
		e.store.RecordFire(e.ctx, watcher.ID)

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
		"content":               content,
		"glyph_id":              glyphID,
		"upstream_attestation":  json.RawMessage(attestationJSON),
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
		"template":              template,
		"glyph_id":              glyphID,
		"upstream_attestation":  json.RawMessage(attestationJSON),
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

// queueRetry adds a failed execution to the retry queue
func (e *Engine) queueRetry(watcherID string, as *types.As, attempt int, lastError string) {
	if attempt > maxRetries {
		e.logger.Warnw("Max retries exceeded, dropping execution",
			"watcher_id", watcherID,
			"attestation_id", as.ID,
			"attempts", attempt)
		return
	}

	// Calculate backoff: 1s, 2s, 4s, 8s, ... up to maxBackoff
	backoff := initialBackoff * time.Duration(1<<(attempt-1))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	e.retryMu.Lock()
	defer e.retryMu.Unlock()

	e.retryQueue = append(e.retryQueue, &PendingExecution{
		WatcherID:   watcherID,
		Attestation: as,
		Attempt:     attempt,
		NextRetryAt: time.Now().Add(backoff),
		LastError:   lastError,
	})

	e.logger.Debugw("Queued for retry",
		"watcher_id", watcherID,
		"attestation_id", as.ID,
		"attempt", attempt,
		"next_retry_at", time.Now().Add(backoff))
}

// retryLoop processes the retry queue
func (e *Engine) retryLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(retryTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.processRetryQueue()
		}
	}
}

// processRetryQueue executes due retries
func (e *Engine) processRetryQueue() {
	now := time.Now()

	e.retryMu.Lock()
	var due []*PendingExecution
	var remaining []*PendingExecution

	for _, pe := range e.retryQueue {
		if pe.NextRetryAt.Before(now) || pe.NextRetryAt.Equal(now) {
			due = append(due, pe)
		} else {
			remaining = append(remaining, pe)
		}
	}
	e.retryQueue = remaining
	e.retryMu.Unlock()

	// Process due items outside the lock
	for _, pe := range due {
		e.mu.RLock()
		watcher, exists := e.watchers[pe.WatcherID]
		e.mu.RUnlock()

		if !exists || !watcher.Enabled {
			continue
		}

		go func(pe *PendingExecution, w *storage.Watcher) {
			var err error

			switch w.ActionType {
			case storage.ActionTypePython:
				err = e.executePython(w, pe.Attestation)
			case storage.ActionTypeWebhook:
				err = e.executeWebhook(w, pe.Attestation)
			case storage.ActionTypeGlyphExecute:
				err = e.executeGlyph(w, pe.Attestation)
			default:
				err = errors.Newf("unknown action type for retry: %s", w.ActionType)
			}

			if err != nil {
				e.logger.Warnw("Retry failed",
					"watcher_id", w.ID,
					"attestation_id", pe.Attestation.ID,
					"attempt", pe.Attempt,
					"error", err)

				e.store.RecordError(e.ctx, w.ID, err.Error())
				e.queueRetry(w.ID, pe.Attestation, pe.Attempt+1, err.Error())
			} else {
				e.logger.Infow("Retry succeeded",
					"watcher_id", w.ID,
					"attestation_id", pe.Attestation.ID,
					"attempt", pe.Attempt)

				e.store.RecordFire(e.ctx, w.ID)
			}
		}(pe, watcher)
	}
}

// GetStore returns the underlying watcher store for CRUD operations
func (e *Engine) GetStore() *storage.WatcherStore {
	return e.store
}

func (e *Engine) DB() *sql.DB {
	return e.db
}
