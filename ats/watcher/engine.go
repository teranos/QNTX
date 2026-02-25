package watcher

import (
	"context"
	"database/sql"
	"encoding/json"
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

// EmbeddingService is the subset of the embedding service needed by the watcher engine.
// Optional — nil when the build does not include embedding support.
type EmbeddingService interface {
	GenerateEmbedding(text string) ([]float32, error)
	ComputeSimilarity(a, b []float32) (float32, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
}

// SemanticSearchResult represents a match from the vector embedding store
type SemanticSearchResult struct {
	SourceID   string
	Text       string
	Similarity float32
}

// EmbeddingSearcher queries pre-computed embeddings via vector similarity (sqlite-vec).
// Used for historical semantic search. Optional — nil when embeddings unavailable.
type EmbeddingSearcher interface {
	Search(queryEmbedding []byte, limit int, threshold float32, clusterID *int) ([]SemanticSearchResult, error)
}

// Engine manages watchers and executes actions when attestations match filters
type Engine struct {
	store  *storage.WatcherStore
	logger *zap.SugaredLogger
	db     *sql.DB // Direct database access for querying historical attestations

	// Base URL for API calls (e.g., "http://localhost:877")
	apiBaseURL string

	// HTTP client with timeout for external calls
	httpClient *http.Client

	// Embedding service for semantic matching (optional, nil when unavailable)
	embeddingService  EmbeddingService
	embeddingSearcher EmbeddingSearcher

	// Broadcast callback for watcher matches (optional)
	// Called when an attestation matches a watcher's filter.
	// score is 0 for structural-only matches, >0 for semantic matches.
	broadcastMatch func(watcherID string, attestation *types.As, score float32)

	// Broadcast callback for glyph execution events (optional)
	// Called when a glyph_execute action fires, with status updates and execution result
	broadcastGlyphFired func(glyphID string, attestationID string, status string, err error, result []byte)

	// In-memory state
	mu              sync.RWMutex
	watchers        map[string]*storage.Watcher
	rateLimiters    map[string]*rate.Limiter
	parseErrors     map[string]error     // Stores parse errors for watchers that failed to load
	queryEmbeddings map[string][]float32 // Pre-computed query embeddings for semantic watchers (watcherID → embedding)

	// Persistent execution queue (replaces in-memory retry)
	queueStore *QueueStore

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

const (
	maxRetries          = 5
	initialBackoff      = 1 * time.Second
	maxBackoff          = 60 * time.Second
	drainInterval       = 200 * time.Millisecond
	drainBatchSize      = 50
	purgeRetention      = 1 * time.Hour
	purgeEveryNthTick   = 100 // purge completed entries every 100th drain tick
)

// NewEngine creates a new watcher engine
func NewEngine(db *sql.DB, apiBaseURL string, logger *zap.SugaredLogger) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		store:      storage.NewWatcherStore(db),
		logger:     logger,
		db:         db,
		apiBaseURL: strings.TrimSuffix(apiBaseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		watchers:        make(map[string]*storage.Watcher),
		rateLimiters:    make(map[string]*rate.Limiter),
		parseErrors:     make(map[string]error),
		queryEmbeddings: make(map[string][]float32),
		queueStore:      NewQueueStore(db),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start loads watchers from DB and starts the drain loop
func (e *Engine) Start() error {
	if err := e.loadWatchers(); err != nil {
		return errors.Wrap(err, "failed to load watchers")
	}

	// Recover any entries that were 'running' when the process died
	orphans, err := e.queueStore.RequeueOrphans()
	if err != nil {
		e.logger.Warnw("Failed to requeue orphaned execution queue entries", "error", err)
	} else if orphans > 0 {
		e.logger.Infow("Requeued orphaned execution queue entries from previous run", "count", orphans)
	}

	// Start drain loop (replaces in-memory retry loop)
	e.wg.Add(1)
	go e.drainLoop()

	e.logger.Infow("Watcher engine started", "watchers_loaded", len(e.watchers))
	return nil
}

// Stop gracefully shuts down the engine. In-flight entries are reset to 'queued'
// so they survive restart.
func (e *Engine) Stop() {
	e.cancel()
	e.wg.Wait()

	// Reset any entries claimed during this drain cycle back to queued
	orphans, err := e.queueStore.RequeueOrphans()
	if err != nil {
		e.logger.Warnw("Failed to requeue in-flight entries during shutdown", "error", err)
	} else if orphans > 0 {
		e.logger.Infow("Reset in-flight queue entries for next startup", "count", orphans)
	}

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
	e.queryEmbeddings = make(map[string][]float32)

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

		// Pre-compute query embedding for semantic watchers
		if w.SemanticQuery != "" && e.embeddingService != nil {
			embedding, err := e.embeddingService.GenerateEmbedding(w.SemanticQuery)
			if err != nil {
				enrichedErr := errors.Wrapf(err, "failed to generate embedding for semantic watcher %s: %q", w.ID, w.SemanticQuery)
				e.logger.Warnw("Failed to generate query embedding, skipping semantic watcher",
					"watcher_id", w.ID,
					"semantic_query", w.SemanticQuery,
					"error", enrichedErr)
				e.parseErrors[w.ID] = enrichedErr
				continue
			}
			e.queryEmbeddings[w.ID] = embedding
			e.logger.Debugw("Cached query embedding for semantic watcher",
				"watcher_id", w.ID,
				"semantic_query", w.SemanticQuery,
				"threshold", w.SemanticThreshold)

			// Compound SE→SE watcher: cache upstream embedding too
			if w.UpstreamSemanticQuery != "" {
				upstreamEmbedding, err := e.embeddingService.GenerateEmbedding(w.UpstreamSemanticQuery)
				if err != nil {
					enrichedErr := errors.Wrapf(err, "failed to generate upstream embedding for compound watcher %s: %q", w.ID, w.UpstreamSemanticQuery)
					e.logger.Warnw("Failed to generate upstream query embedding, skipping compound watcher",
						"watcher_id", w.ID,
						"upstream_query", w.UpstreamSemanticQuery,
						"error", enrichedErr)
					e.parseErrors[w.ID] = enrichedErr
					continue
				}
				e.queryEmbeddings[w.ID+":upstream"] = upstreamEmbedding
				e.logger.Debugw("Cached upstream query embedding for compound watcher",
					"watcher_id", w.ID,
					"upstream_query", w.UpstreamSemanticQuery,
					"upstream_threshold", w.UpstreamSemanticThreshold)
			}
		}

		// Apply edge cursor for meld-edge watchers: skip attestations already processed
		if w.ActionType == storage.ActionTypeGlyphExecute {
			e.applyEdgeCursor(w)
		}

		e.watchers[w.ID] = w
		// If MaxFiresPerSecond is 0, rate is 0 — no fires allowed (QNTX LAW: zero means zero)
		e.rateLimiters[w.ID] = rate.NewLimiter(rate.Limit(float64(w.MaxFiresPerSecond)), 1)
	}

	// Suppress standalone SE watchers that are targets of compound SE→SE melds.
	// The compound watcher handles intersection matching — the standalone must not
	// fire independently or SE₂ would show unfiltered results alongside intersection.
	for _, w := range e.watchers {
		if !strings.HasPrefix(w.ID, "meld-edge-") || w.UpstreamSemanticQuery == "" {
			continue
		}
		var ad struct {
			TargetGlyphID string `json:"target_glyph_id"`
		}
		if err := json.Unmarshal([]byte(w.ActionData), &ad); err != nil {
			e.logger.Debugw("Failed to unmarshal action data during SE suppression",
				"watcher_id", w.ID,
				"error", err)
			continue
		}
		if ad.TargetGlyphID == "" {
			continue
		}
		seID := "se-glyph-" + ad.TargetGlyphID
		se, exists := e.watchers[seID]
		if !exists {
			continue
		}
		// Propagate latest downstream query from the standalone watcher to compound
		if se.SemanticQuery != "" && se.SemanticQuery != w.SemanticQuery {
			w.SemanticQuery = se.SemanticQuery
			w.SemanticThreshold = se.SemanticThreshold
			w.SemanticClusterID = se.SemanticClusterID
			if e.embeddingService != nil {
				if emb, err := e.embeddingService.GenerateEmbedding(se.SemanticQuery); err == nil {
					e.queryEmbeddings[w.ID] = emb
				}
			}
		}
		delete(e.watchers, seID)
		delete(e.queryEmbeddings, seID)
		delete(e.rateLimiters, seID)
		e.logger.Debugw("Suppressed standalone SE watcher (compound target)",
			"se_watcher_id", seID,
			"compound_watcher_id", w.ID)
	}

	return nil
}

// ReloadWatchers reloads watchers from the database (call after CRUD operations)
func (e *Engine) ReloadWatchers() error {
	return e.loadWatchers()
}

// SetBroadcastCallback sets the callback function for broadcasting watcher matches
func (e *Engine) SetBroadcastCallback(callback func(watcherID string, attestation *types.As, score float32)) {
	e.broadcastMatch = callback
}

// SetEmbeddingService sets the optional embedding service for semantic matching.
// Must be called before Start(). When nil, semantic watchers are skipped.
func (e *Engine) SetEmbeddingService(svc EmbeddingService) {
	e.embeddingService = svc
}

// SetEmbeddingSearcher sets the vector similarity searcher for historical semantic queries.
// Uses pre-computed embeddings in the vector DB (sqlite-vec).
func (e *Engine) SetEmbeddingSearcher(searcher EmbeddingSearcher) {
	e.embeddingSearcher = searcher
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

// QueryHistoricalMatches queries historical attestations and broadcasts matches for a watcher.
// For semantic watchers with an embedding searcher available, uses vector DB (sorted by similarity).
// For structural watchers, scans all attestations.
func (e *Engine) QueryHistoricalMatches(watcherID string) error {
	e.mu.RLock()
	watcher, exists := e.watchers[watcherID]
	queryEmbedding := e.queryEmbeddings[watcherID]
	e.mu.RUnlock()

	if !exists {
		return errors.Newf("watcher %s not found in engine", watcherID)
	}

	// Semantic watchers: use pre-computed embeddings via vector DB (fast, sorted by similarity)
	if watcher.SemanticQuery != "" && queryEmbedding != nil && e.embeddingSearcher != nil && e.embeddingService != nil {
		return e.queryHistoricalSemantic(watcherID, watcher, queryEmbedding)
	}

	// Structural watchers: scan all attestations
	return e.queryHistoricalStructural(watcherID, watcher)
}

// queryHistoricalSemantic queries pre-computed embeddings via vector similarity search.
// Returns results sorted by similarity score (highest first).
// For compound SE→SE watchers, searches by upstream query (broader) and post-filters by downstream.
func (e *Engine) queryHistoricalSemantic(watcherID string, watcher *storage.Watcher, queryEmbedding []float32) error {
	// For compound watchers, search by upstream (broader), post-filter by downstream
	searchEmbedding := queryEmbedding
	searchThreshold := watcher.SemanticThreshold
	if searchThreshold <= 0 {
		searchThreshold = 0.3
	}

	isCompound := watcher.UpstreamSemanticQuery != ""
	if isCompound {
		upstreamEmbedding := e.queryEmbeddings[watcherID+":upstream"]
		if upstreamEmbedding != nil {
			searchEmbedding = upstreamEmbedding
			searchThreshold = watcher.UpstreamSemanticThreshold
			if searchThreshold <= 0 {
				searchThreshold = 0.3
			}
		}
	}

	// Serialize query embedding for sqlite-vec
	queryBlob, err := e.embeddingService.SerializeEmbedding(searchEmbedding)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize query embedding for watcher %s", watcherID)
	}

	results, err := e.embeddingSearcher.Search(queryBlob, 50, searchThreshold, watcher.SemanticClusterID)
	if err != nil {
		return errors.Wrapf(err, "failed to search embeddings for watcher %s", watcherID)
	}

	// Load full attestation records for matched source IDs
	matchCount := 0
	for _, result := range results {
		as, err := e.loadAttestation(result.SourceID)
		if err != nil {
			e.logger.Debugw("Failed to load attestation for semantic match",
				"watcher_id", watcherID,
				"source_id", result.SourceID,
				"error", err)
			continue
		}

		// Only broadcast attestations with rich text content
		if extractAttestationText(as) == "" {
			continue
		}

		// For compound watchers, post-filter by downstream query
		downstreamSimilarity := result.Similarity
		if isCompound {
			text := extractAttestationText(as)
			attestationEmbedding, err := e.embeddingService.GenerateEmbedding(text)
			if err != nil {
				continue
			}
			sim, err := e.embeddingService.ComputeSimilarity(queryEmbedding, attestationEmbedding)
			if err != nil {
				continue
			}
			downstreamThreshold := watcher.SemanticThreshold
			if downstreamThreshold <= 0 {
				downstreamThreshold = 0.3
			}
			if sim < downstreamThreshold {
				continue
			}
			downstreamSimilarity = sim
		}

		matchCount++
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcherID, as, downstreamSimilarity)
		}
	}

	e.logger.Infow("Historical semantic query completed",
		"watcher_id", watcherID,
		"matches_found", matchCount,
		"compound", isCompound,
		"threshold", searchThreshold)

	return nil
}

// queryHistoricalStructural scans all attestations and applies structural filters.
func (e *Engine) queryHistoricalStructural(watcherID string, watcher *storage.Watcher) error {
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
		as, err := scanAttestation(rows)
		if err != nil {
			e.logger.Warnw("Failed to scan attestation row",
				"watcher_id", watcherID,
				"error", err)
			continue
		}

		if matched, score := e.matchesWatcher(as, watcher); matched {
			matchCount++
			if e.broadcastMatch != nil {
				e.broadcastMatch(watcherID, as, score)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return errors.Wrap(err, "error iterating attestation rows")
	}

	e.logger.Infow("Historical structural query completed",
		"watcher_id", watcherID,
		"matches_found", matchCount)

	return nil
}

// loadAttestation fetches a single attestation by ID from the database.
func (e *Engine) loadAttestation(id string) (*types.As, error) {
	query := `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes
	          FROM attestations WHERE id = ?`
	row := e.db.QueryRow(query, id)

	var as types.As
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON, attributesJSON []byte

	err := row.Scan(
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
		return nil, errors.Wrapf(err, "failed to load attestation %s", id)
	}

	if err := json.Unmarshal(subjectsJSON, &as.Subjects); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal subjects for %s", id)
	}
	if err := json.Unmarshal(predicatesJSON, &as.Predicates); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal predicates for %s", id)
	}
	if err := json.Unmarshal(contextsJSON, &as.Contexts); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal contexts for %s", id)
	}
	if err := json.Unmarshal(actorsJSON, &as.Actors); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal actors for %s", id)
	}
	if len(attributesJSON) > 0 && string(attributesJSON) != "null" {
		_ = json.Unmarshal(attributesJSON, &as.Attributes)
	}

	return &as, nil
}

// scanAttestation scans a single attestation from a database row.
func scanAttestation(rows *sql.Rows) (*types.As, error) {
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
		return nil, err
	}

	if err := json.Unmarshal(subjectsJSON, &as.Subjects); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(predicatesJSON, &as.Predicates); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(contextsJSON, &as.Contexts); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(actorsJSON, &as.Actors); err != nil {
		return nil, err
	}
	if len(attributesJSON) > 0 && string(attributesJSON) != "null" {
		_ = json.Unmarshal(attributesJSON, &as.Attributes)
	}

	return &as, nil
}

// OnAttestationCreated is called when a new attestation is created.
// Handles structural watchers only — semantic watchers are handled by
// OnAttestationEmbedded after the embedding observer generates the vector.
func (e *Engine) OnAttestationCreated(as *types.As) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, watcher := range e.watchers {
		if !watcher.Enabled {
			continue
		}

		// Skip semantic watchers — handled by OnAttestationEmbedded
		// to avoid redundant GenerateEmbedding FFI calls.
		if watcher.SemanticQuery != "" {
			continue
		}

		matched, score := e.matchesWatcher(as, watcher)
		if !matched {
			continue
		}

		// Broadcast match to frontend (for live results display)
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcher.ID, as, score)
		}

		// Check rate limit for action execution
		// Per QNTX LAW: "Zero means zero" - if MaxFiresPerSecond is 0, never execute
		if watcher.MaxFiresPerSecond == 0 {
			continue
		}
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.enqueueAttestation(watcher.ID, as, "rate_limited", 0, "")
			continue
		}

		// Execute async with a deep copy to prevent race conditions
		asCopy := deepCopyAttestation(as)
		go e.executeAction(watcher, asCopy)
	}
}

// OnAttestationEmbedded is called by the embedding observer after an attestation
// has been embedded and stored. It handles semantic watcher matching using the
// pre-computed attestation embedding, avoiding redundant GenerateEmbedding FFI calls.
func (e *Engine) OnAttestationEmbedded(as *types.As, attestationEmbedding []float32) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.embeddingService == nil {
		return
	}

	for _, watcher := range e.watchers {
		if !watcher.Enabled {
			continue
		}

		// Only process semantic watchers here
		if watcher.SemanticQuery == "" {
			continue
		}

		// Structural filter must also pass (semantic + structural are ANDed)
		if !e.matchesFilter(as, watcher) {
			continue
		}

		// Get cached query embedding for this watcher
		queryEmbedding := e.queryEmbeddings[watcher.ID]
		if queryEmbedding == nil {
			continue
		}

		// Compute cosine similarity (cheap — pure math, no FFI)
		similarity, err := e.embeddingService.ComputeSimilarity(queryEmbedding, attestationEmbedding)
		if err != nil {
			e.logger.Debugw("Failed to compute similarity",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"error", err)
			continue
		}

		threshold := watcher.SemanticThreshold
		if threshold <= 0 {
			threshold = 0.3
		}

		if similarity < threshold {
			continue
		}

		// Compound SE→SE watcher: upstream must also pass
		if watcher.UpstreamSemanticQuery != "" {
			upstreamEmbedding := e.queryEmbeddings[watcher.ID+":upstream"]
			if upstreamEmbedding == nil {
				continue
			}
			upstreamSimilarity, err := e.embeddingService.ComputeSimilarity(upstreamEmbedding, attestationEmbedding)
			if err != nil {
				e.logger.Debugw("Failed to compute upstream similarity",
					"watcher_id", watcher.ID,
					"attestation_id", as.ID,
					"error", err)
				continue
			}
			upstreamThreshold := watcher.UpstreamSemanticThreshold
			if upstreamThreshold <= 0 {
				upstreamThreshold = 0.3
			}
			if upstreamSimilarity < upstreamThreshold {
				continue
			}
			e.logger.Debugw("Compound semantic match (both queries pass)",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"downstream_similarity", similarity,
				"upstream_similarity", upstreamSimilarity)
		} else {
			e.logger.Debugw("Semantic match via pre-computed embedding",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"similarity", similarity,
				"threshold", threshold)
		}

		// Broadcast match to frontend (downstream similarity as score)
		if e.broadcastMatch != nil {
			e.broadcastMatch(watcher.ID, as, similarity)
		}

		// Rate-limited action execution (same logic as OnAttestationCreated)
		if watcher.MaxFiresPerSecond == 0 {
			continue
		}
		limiter := e.rateLimiters[watcher.ID]
		if limiter != nil && !limiter.Allow() {
			e.enqueueAttestation(watcher.ID, as, "rate_limited", 0, "")
			continue
		}

		asCopy := deepCopyAttestation(as)
		go e.executeAction(watcher, asCopy)
	}
}

// matchesWatcher checks if an attestation matches a watcher using the appropriate strategy.
// Structural filters (AxQuery / Filter fields) and semantic queries are ANDed:
// if both are set, both must pass.
// Returns (matched, similarity score). Score is 0 for structural-only matches.
func (e *Engine) matchesWatcher(as *types.As, watcher *storage.Watcher) (bool, float32) {
	// Structural filter check (empty filter passes all)
	if !e.matchesFilter(as, watcher) {
		return false, 0
	}

	// Semantic check — only if this watcher has a query embedding cached
	if _, hasSemantic := e.queryEmbeddings[watcher.ID]; hasSemantic {
		return e.matchesSemantic(as, watcher)
	}

	// No semantic embedding cached — try lazy initialization if embedding service
	// is now available (it may have been nil at loadWatchers time).
	// NOTE: called under RLock — schedule async caching, use one-shot embedding for this match.
	if watcher.SemanticQuery != "" {
		if e.embeddingService != nil {
			embedding, err := e.embeddingService.GenerateEmbedding(watcher.SemanticQuery)
			if err == nil {
				// Cache for future calls (needs write lock — do async to avoid deadlock)
				go func(id string, emb []float32) {
					e.mu.Lock()
					e.queryEmbeddings[id] = emb
					e.mu.Unlock()
					e.logger.Infow("Lazy-initialized query embedding for semantic watcher",
						"watcher_id", id)
				}(watcher.ID, embedding)
				// Use the embedding for this match immediately
				return e.matchesSemanticWithEmbedding(as, watcher, embedding)
			}
			e.logger.Debugw("Lazy embedding generation failed for semantic watcher",
				"watcher_id", watcher.ID,
				"semantic_query", watcher.SemanticQuery,
				"error", err)
		}
		return false, 0
	}

	return true, 0
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

// matchesSemantic checks using the cached query embedding.
func (e *Engine) matchesSemantic(as *types.As, watcher *storage.Watcher) (bool, float32) {
	queryEmbedding := e.queryEmbeddings[watcher.ID]
	if queryEmbedding == nil {
		return false, 0
	}
	return e.matchesSemanticWithEmbedding(as, watcher, queryEmbedding)
}

// matchesSemanticWithEmbedding checks if an attestation's text content is semantically
// similar to a given query embedding. Used by both cached and lazy-init paths.
func (e *Engine) matchesSemanticWithEmbedding(as *types.As, watcher *storage.Watcher, queryEmbedding []float32) (bool, float32) {
	if e.embeddingService == nil {
		return false, 0
	}

	text := extractAttestationText(as)
	if text == "" {
		return false, 0
	}

	attestationEmbedding, err := e.embeddingService.GenerateEmbedding(text)
	if err != nil {
		e.logger.Debugw("Failed to generate embedding for attestation",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
		return false, 0
	}

	similarity, err := e.embeddingService.ComputeSimilarity(queryEmbedding, attestationEmbedding)
	if err != nil {
		e.logger.Debugw("Failed to compute similarity",
			"watcher_id", watcher.ID,
			"attestation_id", as.ID,
			"error", err)
		return false, 0
	}

	threshold := watcher.SemanticThreshold
	if threshold <= 0 {
		threshold = 0.3 // Default threshold
	}

	matches := similarity >= threshold
	e.logger.Debugw("Semantic comparison",
		"watcher_id", watcher.ID,
		"attestation_id", as.ID,
		"similarity", similarity,
		"threshold", threshold,
		"matches", matches)

	return matches, similarity
}

// extractAttestationText returns rich text from an attestation's attributes.
// Returns empty string for structural-only attestations — semantic search
// only applies to attestations with rich text content.
// Skips metadata keys (rich_string_fields) that contain field names, not content.
func extractAttestationText(as *types.As) string {
	if as.Attributes == nil {
		return ""
	}

	var parts []string
	for key, value := range as.Attributes {
		if key == "rich_string_fields" {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok && str != "" {
					parts = append(parts, str)
				}
			}
		}
	}

	return strings.Join(parts, " ")
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

// enqueueAttestation serializes an attestation and inserts it into the persistent queue.
func (e *Engine) enqueueAttestation(watcherID string, as *types.As, reason string, attempt int, lastError string) {
	if reason == "retry" && attempt > maxRetries {
		e.logger.Warnw("Max retries exceeded, giving up",
			"watcher_id", watcherID,
			"attestation_id", as.ID,
			"attempts", attempt)
		return
	}

	attestationJSON, err := json.Marshal(as)
	if err != nil {
		e.logger.Errorw("Failed to serialize attestation for queue",
			"watcher_id", watcherID,
			"attestation_id", as.ID,
			"error", err)
		return
	}

	notBefore := time.Now()
	if reason == "retry" && attempt > 0 {
		backoff := initialBackoff * time.Duration(1<<(attempt-1))
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		notBefore = notBefore.Add(backoff)
	}

	entry := &QueueEntry{
		WatcherID:       watcherID,
		AttestationJSON: string(attestationJSON),
		Reason:          reason,
		Attempt:         attempt,
		NotBefore:       notBefore,
		LastError:       lastError,
	}

	if err := e.queueStore.Enqueue(entry); err != nil {
		e.logger.Errorw("Failed to enqueue execution",
			"watcher_id", watcherID,
			"attestation_id", as.ID,
			"reason", reason,
			"error", err)
		return
	}

	e.logger.Debugw("Enqueued attestation for deferred execution",
		"watcher_id", watcherID,
		"attestation_id", as.ID,
		"reason", reason,
		"attempt", attempt,
		"not_before", notBefore)
}

// drainLoop processes the persistent execution queue at a fixed interval.
func (e *Engine) drainLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(drainInterval)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.drainOnce()
			tickCount++
			if tickCount%purgeEveryNthTick == 0 {
				purged, err := e.queueStore.PurgeCompleted(purgeRetention)
				if err != nil {
					e.logger.Warnw("Failed to purge completed queue entries", "error", err)
				} else if purged > 0 {
					e.logger.Debugw("Purged completed queue entries", "count", purged)
				}
			}
		}
	}
}

// drainOnce dequeues and processes one batch of entries (one per watcher, round-robin).
func (e *Engine) drainOnce() {
	entries, err := e.queueStore.DequeueRoundRobin(time.Now(), drainBatchSize)
	if err != nil {
		e.logger.Warnw("Failed to dequeue from execution queue", "error", err)
		return
	}

	for _, entry := range entries {

		e.mu.RLock()
		watcher, exists := e.watchers[entry.WatcherID]
		limiter := e.rateLimiters[entry.WatcherID]
		e.mu.RUnlock()

		if !exists || !watcher.Enabled {
			e.queueStore.Complete(entry.ID)
			continue
		}

		// For rate-limited entries, use Reserve/Cancel to peek at when the next token is available
		if entry.Reason == "rate_limited" {
			if limiter != nil {
				r := limiter.Reserve()
				delay := r.Delay()
				if delay > 0 {
					// Token not available yet — cancel reservation and defer to exact time
					r.Cancel()
					retryAfter := time.Now().Add(delay)
					e.queueStore.Requeue(entry.ID, retryAfter)
					continue
				}
				// delay == 0: token consumed by Reserve, proceed with execution
			}
		}

		// Deserialize attestation
		var as types.As
		if err := json.Unmarshal([]byte(entry.AttestationJSON), &as); err != nil {
			e.logger.Errorw("Failed to deserialize queued attestation",
				"queue_id", entry.ID,
				"watcher_id", entry.WatcherID,
				"error", err)
			e.queueStore.Fail(entry.ID, err.Error())
			continue
		}

		// Execute the action
		var execErr error
		switch watcher.ActionType {
		case storage.ActionTypePython:
			execErr = e.executePython(watcher, &as)
		case storage.ActionTypeWebhook:
			execErr = e.executeWebhook(watcher, &as)
		case storage.ActionTypeGlyphExecute:
			execErr = e.executeGlyph(watcher, &as)
		case storage.ActionTypeSemanticMatch:
			e.queueStore.Complete(entry.ID)
			continue
		default:
			execErr = errors.Newf("unknown action type: %s", watcher.ActionType)
		}

		if execErr != nil {
			e.logger.Warnw("Queued execution failed",
				"watcher_id", watcher.ID,
				"attestation_id", as.ID,
				"attempt", entry.Attempt,
				"error", execErr)

			e.store.RecordError(e.ctx, watcher.ID, execErr.Error())
			e.queueStore.Complete(entry.ID)

			// Re-enqueue as retry with incremented attempt and backoff
			e.enqueueAttestation(watcher.ID, &as, "retry", entry.Attempt+1, execErr.Error())
		} else {
			e.store.RecordFire(e.ctx, watcher.ID)
			e.queueStore.Complete(entry.ID)

			if watcher.ActionType == storage.ActionTypeGlyphExecute {
				e.updateEdgeCursor(watcher, &as)
			}
		}
	}
}

// deepCopyAttestation creates a deep copy of an attestation to prevent race conditions.
func deepCopyAttestation(as *types.As) *types.As {
	asCopy := *as
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
	return &asCopy
}

// GetQueueStore returns the queue store for external access (stats endpoint).
func (e *Engine) GetQueueStore() *QueueStore {
	return e.queueStore
}

// GetStore returns the underlying watcher store for CRUD operations
func (e *Engine) GetStore() *storage.WatcherStore {
	return e.store
}

func (e *Engine) DB() *sql.DB {
	return e.db
}
