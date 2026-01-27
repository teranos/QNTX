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

	// Base URL for API calls (e.g., "http://localhost:877")
	apiBaseURL string

	// In-memory state
	mu           sync.RWMutex
	watchers     map[string]*storage.Watcher
	rateLimiters map[string]*rate.Limiter

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
		apiBaseURL:   strings.TrimSuffix(apiBaseURL, "/"),
		watchers:     make(map[string]*storage.Watcher),
		rateLimiters: make(map[string]*rate.Limiter),
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

// loadWatchers loads all enabled watchers from the database
func (e *Engine) loadWatchers() error {
	watchers, err := e.store.List(true) // enabled only
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, w := range watchers {
		e.watchers[w.ID] = w
		e.rateLimiters[w.ID] = rate.NewLimiter(rate.Limit(float64(w.MaxFiresPerMinute)/60.0), 1)
	}

	return nil
}

// ReloadWatchers reloads watchers from the database (call after CRUD operations)
func (e *Engine) ReloadWatchers() error {
	return e.loadWatchers()
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

		if !matchesFilter(as, &watcher.Filter) {
			continue
		}

		// Check rate limit
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.logger.Debugw("Watcher rate limited",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID)
			continue
		}

		// Execute async
		go e.executeAction(watcher, as)
	}
}

// matchesFilter checks if an attestation matches a watcher's filter (exact matching only)
func matchesFilter(as *types.As, filter *types.AxFilter) bool {
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
	var err error

	switch watcher.ActionType {
	case storage.ActionTypePython:
		err = e.executePython(watcher, as)
	case storage.ActionTypeWebhook:
		err = e.executeWebhook(watcher, as)
	default:
		err = errors.Newf("unknown action type: %s", watcher.ActionType)
	}

	if err != nil {
		e.logger.Errorw("Watcher action failed",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)

		// Record error
		e.store.RecordError(watcher.ID, err.Error())

		// Queue for retry
		e.queueRetry(watcher.ID, as, 1, err.Error())
	} else {
		e.logger.Infow("Watcher action succeeded",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID)

		// Record success
		e.store.RecordFire(watcher.ID)
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
		"code": injectedCode,
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

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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
			}

			if err != nil {
				e.logger.Warnw("Retry failed",
					"watcher_id", w.ID,
					"attestation_id", pe.Attestation.ID,
					"attempt", pe.Attempt,
					"error", err)

				e.store.RecordError(w.ID, err.Error())
				e.queueRetry(w.ID, pe.Attestation, pe.Attempt+1, err.Error())
			} else {
				e.logger.Infow("Retry succeeded",
					"watcher_id", w.ID,
					"attestation_id", pe.Attestation.ID,
					"attempt", pe.Attempt)

				e.store.RecordFire(w.ID)
			}
		}(pe, watcher)
	}
}

// GetStore returns the underlying watcher store for CRUD operations
func (e *Engine) GetStore() *storage.WatcherStore {
	return e.store
}
