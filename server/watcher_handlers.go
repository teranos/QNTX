package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/errors"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
)

// WatcherCreateRequest represents a request to create a new watcher
type WatcherCreateRequest struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Subjects          []string `json:"subjects,omitempty"`
	Predicates        []string `json:"predicates,omitempty"`
	Contexts          []string `json:"contexts,omitempty"`
	Actors            []string `json:"actors,omitempty"`
	TimeStart         string   `json:"time_start,omitempty"` // RFC3339
	TimeEnd           string   `json:"time_end,omitempty"`   // RFC3339
	ActionType        string   `json:"action_type"`          // "python", "webhook", or "semantic_match"
	ActionData        string   `json:"action_data"`          // Python code or webhook URL (not required for semantic_match)
	MaxFiresPerSecond int      `json:"max_fires_per_second,omitempty"`
	Enabled           *bool    `json:"enabled,omitempty"`
	// Semantic matching fields (for ⊨ glyphs)
	SemanticQuery     string  `json:"semantic_query,omitempty"`
	SemanticThreshold float32 `json:"semantic_threshold,omitempty"`
}

// WatcherResponse represents a watcher in API responses
type WatcherResponse struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Subjects          []string `json:"subjects,omitempty"`
	Predicates        []string `json:"predicates,omitempty"`
	Contexts          []string `json:"contexts,omitempty"`
	Actors            []string `json:"actors,omitempty"`
	TimeStart         string   `json:"time_start,omitempty"`
	TimeEnd           string   `json:"time_end,omitempty"`
	ActionType        string   `json:"action_type"`
	ActionData        string   `json:"action_data"`
	SemanticQuery     string   `json:"semantic_query,omitempty"`
	SemanticThreshold float32  `json:"semantic_threshold,omitempty"`
	MaxFiresPerSecond int      `json:"max_fires_per_second"`
	Enabled           bool     `json:"enabled"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	LastFiredAt       string   `json:"last_fired_at,omitempty"`
	FireCount         int64    `json:"fire_count"`
	ErrorCount        int64    `json:"error_count"`
	LastError         string   `json:"last_error,omitempty"`
}

// HandleWatchers handles watcher CRUD operations
// Routes:
//
//	GET    /api/watchers       - List all watchers
//	POST   /api/watchers       - Create a new watcher
//	GET    /api/watchers/{id}  - Get a watcher by ID
//	PUT    /api/watchers/{id}  - Update a watcher
//	DELETE /api/watchers/{id}  - Delete a watcher
func (s *QNTXServer) HandleWatchers(w http.ResponseWriter, r *http.Request) {
	if s.watcherEngine == nil {
		s.writeRichError(w, errors.New("watcher engine not initialized"), http.StatusServiceUnavailable)
		return
	}

	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/watchers")
	path = strings.TrimPrefix(path, "/")
	watcherID := path

	switch r.Method {
	case http.MethodGet:
		if watcherID == "" {
			s.handleListWatchers(w, r)
		} else {
			s.handleGetWatcher(w, r, watcherID)
		}
	case http.MethodPost:
		s.handleCreateWatcher(w, r)
	case http.MethodPut:
		if watcherID == "" {
			http.Error(w, "Watcher ID required", http.StatusBadRequest)
			return
		}
		s.handleUpdateWatcher(w, r, watcherID)
	case http.MethodDelete:
		if watcherID == "" {
			http.Error(w, "Watcher ID required", http.StatusBadRequest)
			return
		}
		s.handleDeleteWatcher(w, r, watcherID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *QNTXServer) handleListWatchers(w http.ResponseWriter, r *http.Request) {
	enabledOnly := r.URL.Query().Get("enabled") == "true"

	watchers, err := s.watcherEngine.GetStore().List(r.Context(), enabledOnly)
	if err != nil {
		s.writeRichError(w, errors.Wrap(err, "failed to list watchers"), http.StatusInternalServerError)
		return
	}

	response := make([]WatcherResponse, len(watchers))
	for i, watcher := range watchers {
		response[i] = watcherToResponse(watcher)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *QNTXServer) handleGetWatcher(w http.ResponseWriter, r *http.Request, id string) {
	watcher, err := s.watcherEngine.GetStore().Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeRichError(w, err, http.StatusNotFound)
		} else {
			s.writeRichError(w, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(watcherToResponse(watcher))
}

func (s *QNTXServer) handleCreateWatcher(w http.ResponseWriter, r *http.Request) {
	var req WatcherCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRichError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ID == "" {
		s.writeRichError(w, errors.New("id is required"), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		s.writeRichError(w, errors.New("name is required"), http.StatusBadRequest)
		return
	}
	if req.ActionType == "" {
		s.writeRichError(w, errors.New("action_type is required"), http.StatusBadRequest)
		return
	}
	switch req.ActionType {
	case "python", "webhook":
		if req.ActionData == "" {
			s.writeRichError(w, errors.New("action_data is required for python/webhook watchers"), http.StatusBadRequest)
			return
		}
	case "plugin_execute", "glyph_execute":
		if req.ActionData == "" {
			s.writeRichError(w, errors.Newf("action_data is required for %s watchers", req.ActionType), http.StatusBadRequest)
			return
		}
	case "semantic_match":
		if req.SemanticQuery == "" {
			s.writeRichError(w, errors.New("semantic_query is required for semantic_match watchers"), http.StatusBadRequest)
			return
		}
	default:
		s.writeRichError(w, errors.Newf("invalid action_type: %s", req.ActionType), http.StatusBadRequest)
		return
	}

	// Build watcher
	watcher := &storage.Watcher{
		ID:   req.ID,
		Name: req.Name,
		Filter: types.AxFilter{
			Subjects:   req.Subjects,
			Predicates: req.Predicates,
			Contexts:   req.Contexts,
			Actors:     req.Actors,
		},
		ActionType:        storage.ActionType(req.ActionType),
		ActionData:        req.ActionData,
		SemanticQuery:     req.SemanticQuery,
		SemanticThreshold: req.SemanticThreshold,
		MaxFiresPerSecond: am.GetInt("watcher.max_fires_per_second"),
		Enabled:           true,
	}

	if req.MaxFiresPerSecond > 0 {
		watcher.MaxFiresPerSecond = req.MaxFiresPerSecond
	}
	if req.Enabled != nil {
		watcher.Enabled = *req.Enabled
	}

	// Parse time filters
	if req.TimeStart != "" {
		t, err := time.Parse(time.RFC3339, req.TimeStart)
		if err != nil {
			s.writeRichError(w, errors.Wrap(err, "invalid time_start format (use RFC3339)"), http.StatusBadRequest)
			return
		}
		watcher.Filter.TimeStart = &t
	}
	if req.TimeEnd != "" {
		t, err := time.Parse(time.RFC3339, req.TimeEnd)
		if err != nil {
			s.writeRichError(w, errors.Wrap(err, "invalid time_end format (use RFC3339)"), http.StatusBadRequest)
			return
		}
		watcher.Filter.TimeEnd = &t
	}

	// Create watcher
	if err := s.watcherEngine.GetStore().Create(r.Context(), watcher); err != nil {
		s.writeRichError(w, errors.Wrap(err, "failed to create watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := s.watcherEngine.ReloadWatchers(); err != nil {
		s.logger.Warnw("Failed to reload watchers after create", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(watcherToResponse(watcher))
}

func (s *QNTXServer) handleUpdateWatcher(w http.ResponseWriter, r *http.Request, id string) {
	// Get existing watcher
	existing, err := s.watcherEngine.GetStore().Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeRichError(w, err, http.StatusNotFound)
		} else {
			s.writeRichError(w, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	var req WatcherCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRichError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	// Update fields if provided
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Subjects != nil {
		existing.Filter.Subjects = req.Subjects
	}
	if req.Predicates != nil {
		existing.Filter.Predicates = req.Predicates
	}
	if req.Contexts != nil {
		existing.Filter.Contexts = req.Contexts
	}
	if req.Actors != nil {
		existing.Filter.Actors = req.Actors
	}
	if req.ActionType != "" {
		existing.ActionType = storage.ActionType(req.ActionType)
	}
	if req.ActionData != "" {
		existing.ActionData = req.ActionData
	}
	if req.MaxFiresPerSecond > 0 {
		existing.MaxFiresPerSecond = req.MaxFiresPerSecond
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.TimeStart != "" {
		t, err := time.Parse(time.RFC3339, req.TimeStart)
		if err != nil {
			s.writeRichError(w, errors.Wrap(err, "invalid time_start format"), http.StatusBadRequest)
			return
		}
		existing.Filter.TimeStart = &t
	}
	if req.TimeEnd != "" {
		t, err := time.Parse(time.RFC3339, req.TimeEnd)
		if err != nil {
			s.writeRichError(w, errors.Wrap(err, "invalid time_end format"), http.StatusBadRequest)
			return
		}
		existing.Filter.TimeEnd = &t
	}
	if req.SemanticQuery != "" {
		existing.SemanticQuery = req.SemanticQuery
	}
	if req.SemanticThreshold > 0 {
		existing.SemanticThreshold = req.SemanticThreshold
	}

	// Update in DB
	if err := s.watcherEngine.GetStore().Update(r.Context(), existing); err != nil {
		s.writeRichError(w, errors.Wrap(err, "failed to update watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := s.watcherEngine.ReloadWatchers(); err != nil {
		s.logger.Warnw("Failed to reload watchers after update", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(watcherToResponse(existing))
}

func (s *QNTXServer) handleDeleteWatcher(w http.ResponseWriter, r *http.Request, id string) {
	// Verify watcher exists
	if _, err := s.watcherEngine.GetStore().Get(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeRichError(w, err, http.StatusNotFound)
		} else {
			s.writeRichError(w, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	// Delete
	if err := s.watcherEngine.GetStore().Delete(r.Context(), id); err != nil {
		s.writeRichError(w, errors.Wrap(err, "failed to delete watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := s.watcherEngine.ReloadWatchers(); err != nil {
		s.logger.Warnw("Failed to reload watchers after delete", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleWatcherQueueStats returns execution queue statistics
func (s *QNTXServer) HandleWatcherQueueStats(w http.ResponseWriter, r *http.Request) {
	if s.watcherEngine == nil {
		s.writeRichError(w, errors.New("watcher engine not initialized"), http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := s.watcherEngine.GetQueueStore().Stats()
	if err != nil {
		s.writeRichError(w, errors.Wrap(err, "failed to get queue stats"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// watcherToResponse converts a storage.Watcher to a WatcherResponse
func watcherToResponse(w *storage.Watcher) WatcherResponse {
	resp := WatcherResponse{
		ID:                w.ID,
		Name:              w.Name,
		Subjects:          w.Filter.Subjects,
		Predicates:        w.Filter.Predicates,
		Contexts:          w.Filter.Contexts,
		Actors:            w.Filter.Actors,
		ActionType:        string(w.ActionType),
		ActionData:        w.ActionData,
		SemanticQuery:     w.SemanticQuery,
		SemanticThreshold: w.SemanticThreshold,
		MaxFiresPerSecond: w.MaxFiresPerSecond,
		Enabled:           w.Enabled,
		CreatedAt:         w.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         w.UpdatedAt.Format(time.RFC3339),
		FireCount:         w.FireCount,
		ErrorCount:        w.ErrorCount,
		LastError:         w.LastError,
	}

	if w.Filter.TimeStart != nil {
		resp.TimeStart = w.Filter.TimeStart.Format(time.RFC3339)
	}
	if w.Filter.TimeEnd != nil {
		resp.TimeEnd = w.Filter.TimeEnd.Format(time.RFC3339)
	}
	if w.LastFiredAt != nil {
		resp.LastFiredAt = w.LastFiredAt.Format(time.RFC3339)
	}

	return resp
}

// broadcastWatcherMatch broadcasts a watcher match to all connected clients
func (s *QNTXServer) broadcastWatcherMatch(watcherID string, attestation *types.As, score float32) {
	msg := WatcherMatchMessage{
		Type:        "watcher_match",
		WatcherID:   watcherID,
		Attestation: attestation,
		Score:       score,
		Timestamp:   time.Now().Unix(),
	}

	// For meld-edge watchers, extract target glyph ID from action data
	// so the frontend can route matches to the correct glyph
	if strings.HasPrefix(watcherID, "meld-edge-") {
		if w, exists := s.watcherEngine.GetWatcher(watcherID); exists {
			var actionData struct {
				TargetGlyphID string `json:"target_glyph_id"`
			}
			if json.Unmarshal([]byte(w.ActionData), &actionData) == nil && actionData.TargetGlyphID != "" {
				msg.TargetGlyphID = actionData.TargetGlyphID
			}
		}
	}

	// Send to all clients via broadcast worker
	req := &broadcastRequest{
		reqType: "watcher_match",
		payload: msg,
	}

	select {
	case s.broadcastReq <- req:
		s.logger.Debugw("Broadcast watcher match",
			"watcher_id", watcherID,
			"attestation_id", attestation.ID)
	case <-s.ctx.Done():
		// Server shutting down
	default:
		s.logger.Warnw("Broadcast request queue full, dropping watcher match",
			"watcher_id", watcherID,
			"attestation_id", attestation.ID)
	}
}

// broadcastWatcherError broadcasts a watcher error to all connected clients.
// Used to send parsing errors, validation errors, etc. to the UI for immediate feedback.
// Accepts an optional details slice for structured error context (from errors.GetAllDetails).
func (s *QNTXServer) broadcastWatcherError(watcherID string, errorMsg string, severity string, details ...string) {
	msg := WatcherErrorMessage{
		Type:      "watcher_error",
		WatcherID: watcherID,
		Error:     errorMsg,
		Details:   details,
		Severity:  severity,
		Timestamp: time.Now().Unix(),
	}

	// Send to all clients via broadcast worker
	req := &broadcastRequest{
		reqType: "watcher_error",
		payload: msg,
	}

	select {
	case s.broadcastReq <- req:
		s.logger.Debugw("Broadcast watcher error",
			"watcher_id", watcherID,
			"error", errorMsg,
			"details", details,
			"severity", severity)
	case <-s.ctx.Done():
		// Server shutting down
	default:
		s.logger.Warnw("Broadcast request queue full, dropping watcher error",
			"watcher_id", watcherID)
	}
}

// broadcastGlyphFired broadcasts a glyph execution event to all connected clients
func (s *QNTXServer) broadcastGlyphFired(glyphID string, attestationID string, status string, execErr error, result []byte) {
	msg := GlyphFiredMessage{
		Type:          "glyph_fired",
		GlyphID:       glyphID,
		AttestationID: attestationID,
		Status:        status,
		Timestamp:     time.Now().Unix(),
	}
	if execErr != nil {
		msg.Error = execErr.Error()
	}
	if len(result) > 0 {
		msg.Result = string(result)
	}

	req := &broadcastRequest{
		reqType: "glyph_fired",
		payload: msg,
	}

	select {
	case s.broadcastReq <- req:
		s.logger.Debugw("Broadcast glyph fired",
			"glyph_id", glyphID,
			"attestation_id", attestationID,
			"status", status)
	case <-s.ctx.Done():
	}
}

// initWatcherEngine initializes the watcher engine and registers it as an observer
func (s *QNTXServer) initWatcherEngine() error {
	apiBaseURL := fmt.Sprintf("http://127.0.0.1:%d", am.GetServerPort())

	// Open a separate DB connection for watcher engine operations (enqueue, recordFire,
	// edge cursors). This eliminates contention with the main RustStore connection —
	// without it, watcher goroutines pile up waiting for the single MaxOpenConns(1) slot,
	// blocking attestation writes for 5+ seconds during high-volume crawls.
	watcherDB, err := sql.Open("rustsqlite", s.dbPath)
	if err != nil {
		return errors.Wrap(err, "failed to open watcher DB connection")
	}
	watcherDB.SetMaxOpenConns(4)
	s.watcherDB = watcherDB
	rustdriver.SetCaller("watcher-db")

	// Pass atsStore as AttestationReader so watcher queries go through Rust's connection,
	// eliminating dual-driver access to the attestations table.
	reader, _ := s.atsStore.(watcher.AttestationReader)
	s.watcherEngine = watcher.NewEngine(watcherDB, reader, apiBaseURL, s.logger)
	s.reloadCoalescer = newWatcherReloadCoalescer(s, 50*time.Millisecond)

	// Built-in glyph types. Plugin-provided types (e.g. "py") are registered
	// dynamically when plugins declare python_provider=true during Initialize.
	s.watcherEngine.SetAvailableGlyphTypes([]string{"prompt", "se"})

	// Set broadcast callback for live results
	s.watcherEngine.SetBroadcastCallback(s.broadcastWatcherMatch)

	// Set glyph fired callback for meld-triggered execution feedback
	s.watcherEngine.SetGlyphFiredCallback(s.broadcastGlyphFired)

	// Wire plugin executor for plugin_execute action type
	s.watcherEngine.SetPluginExecutor(&watcherPluginAdapter{server: s})

	// Wire embedding service for semantic matching (optional — nil when embeddings unavailable)
	// Note: embeddingService may be nil here if SetupEmbeddingService() hasn't run yet.
	// In that case, init.go reconnects after embedding init.
	if s.embeddingService != nil {
		s.watcherEngine.SetEmbeddingService(&watcherEmbeddingAdapter{svc: s.embeddingService})
		if s.embeddingStore != nil {
			s.watcherEngine.SetEmbeddingSearcher(&watcherSearchAdapter{store: s.embeddingStore})
		}
	}

	// Register as global observer (notified on all attestation creations)
	storage.RegisterObserver(s.watcherEngine)

	// Start the engine
	if err := s.watcherEngine.Start(); err != nil {
		return errors.Wrap(err, "failed to start watcher engine")
	}

	// Start dilation loop: adjusts watcher firing rates based on system memory pressure
	go s.runDilationLoop()

	s.logger.Debug("Watcher engine initialized")
	return nil
}

// dilationLevels are the possible dilation values, ordered high to low for display.
var dilationLevels = []float64{2.0, 1.5, 1.25, 1.0, 0.75, 0.5, 0.25, 0.1, 0.0}

// runDilationLoop samples system pressure every 10s and adjusts watcher firing rates.
// Logging schedule: first at ~2min (after plugins load), then every 30min.
// Each log shows a distribution of dilation values over the window.
func (s *QNTXServer) runDilationLoop() {
	const (
		sampleInterval = 10 * time.Second
		earlyLogAfter  = 3 * time.Minute
		steadyLogEvery = 30 * time.Minute
		maxBarWidth    = 8

		colorGreen  = "\033[32m"
		colorYellow = "\033[33m"
		colorRed    = "\033[31m"
		colorReset  = "\033[0m"
	)

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	var (
		dist       = make(map[float64]int)
		lastLogged = time.Now()
		earlyDone  bool
	)

	resetDist := func() {
		for k := range dist {
			delete(dist, k)
		}
		lastLogged = time.Now()
	}

	dilationColor := func(level float64) string {
		switch {
		case level >= 1.5:
			return colorGreen
		case level >= 0.75:
			return colorYellow
		default:
			return colorRed
		}
	}

	formatDist := func(d, memPct, cpuPct float64, tag string) string {
		total := 0
		for _, n := range dist {
			total += n
		}
		if total == 0 {
			total = 1
		}

		var b strings.Builder
		b.WriteString(fmt.Sprintf("Dilation %s  now=%.2f  mem=%.1f%%  cpu=%.1f%%\n  ", tag, d, memPct, cpuPct))
		first := true
		for _, level := range dilationLevels {
			n := dist[level]
			if n == 0 {
				continue
			}
			pct := float64(n) / float64(total) * 100
			filled := int(pct / 100 * maxBarWidth)
			if filled == 0 {
				filled = 1
			}
			if !first {
				b.WriteString("  ")
			}
			bar := strings.Repeat("\u2593", filled)
			color := dilationColor(level)
			b.WriteString(fmt.Sprintf("%.2f %s%s%s %0.0f%%", level, color, bar, colorReset, pct))
			first = false
		}
		return b.String()
	}

	logDilation := func(d, memPct, cpuPct float64, tag string) {
		s.logger.Debugf("\n%s", formatDist(d, memPct, cpuPct, tag))
		resetDist()
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.watcherEngine == nil {
				continue
			}

			d := async.CalculateDilation()
			memPct, cpuPct := async.GetPressure()
			prev := s.watcherEngine.Dilation()
			if d != prev {
				s.watcherEngine.SetDilation(d)
			}

			dist[d]++
			elapsed := time.Since(lastLogged)

			if !earlyDone && elapsed >= earlyLogAfter {
				logDilation(d, memPct, cpuPct, "early")
				earlyDone = true
				continue
			}

			// Steady state: every 30 minutes
			if elapsed >= steadyLogEvery {
				logDilation(d, memPct, cpuPct, "steady")
			}
		}
	}
}

// watcherEmbeddingAdapter adapts the server's embedding service (which returns
// *serverembeddings.EmbeddingResult) to the watcher engine's simpler interface.
type watcherEmbeddingAdapter struct {
	svc interface {
		GenerateEmbedding(text, model string) (*serverembeddings.EmbeddingResult, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
	}
}

func (a *watcherEmbeddingAdapter) GenerateEmbedding(text string) ([]float32, error) {
	result, err := a.svc.GenerateEmbedding(text, "")
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (a *watcherEmbeddingAdapter) ComputeSimilarity(x, y []float32) (float32, error) {
	return a.svc.ComputeSimilarity(x, y)
}

func (a *watcherEmbeddingAdapter) SerializeEmbedding(embedding []float32) ([]byte, error) {
	return a.svc.SerializeEmbedding(embedding)
}

// watcherSearchAdapter adapts the storage.EmbeddingStore to the watcher engine's EmbeddingSearcher.
type watcherSearchAdapter struct {
	store *storage.EmbeddingStore
}

func (a *watcherSearchAdapter) Search(queryEmbedding []byte, limit int, threshold float32, clusterID *int) ([]watcher.SemanticSearchResult, error) {
	results, err := a.store.SemanticSearch(queryEmbedding, limit, threshold, clusterID, "")
	if err != nil {
		return nil, err
	}
	out := make([]watcher.SemanticSearchResult, 0, len(results))
	for _, r := range results {
		if r.SourceType == "attestation" {
			out = append(out, watcher.SemanticSearchResult{
				SourceID:   r.SourceID,
				Text:       r.Text,
				Similarity: r.Similarity,
			})
		}
	}
	return out, nil
}

// watcherPluginAdapter adapts the server's plugin manager to the watcher engine's PluginExecutor interface.
type watcherPluginAdapter struct {
	server *QNTXServer
}

func (a *watcherPluginAdapter) ExecutePluginJob(ctx context.Context, pluginName string, handlerName string, payload []byte) ([]byte, error) {
	pm := a.server.getPluginManager()
	if pm == nil {
		return nil, errors.Newf("no plugin manager available, cannot execute plugin %s", pluginName)
	}

	dp, ok := pm.GetPlugin(pluginName)
	if !ok {
		return nil, errors.Newf("plugin %q not found", pluginName)
	}

	proxy, ok := dp.(*grpcplugin.ExternalDomainProxy)
	if !ok {
		return nil, errors.Newf("plugin %q is not a gRPC plugin", pluginName)
	}

	resp, err := proxy.Client().ExecuteJob(ctx, &protocol.ExecuteJobRequest{
		JobId:       fmt.Sprintf("watcher-%d", time.Now().UnixNano()),
		HandlerName: handlerName,
		Payload:     payload,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC ExecuteJob failed for plugin %s handler %s", pluginName, handlerName)
	}

	if !resp.Success {
		return nil, errors.Newf("plugin %s handler %s returned error: %s", pluginName, handlerName, resp.Error)
	}

	return resp.Result, nil
}

func (a *watcherPluginAdapter) IsPluginLoaded(pluginName string) bool {
	pm := a.server.getPluginManager()
	if pm == nil {
		return false
	}
	_, ok := pm.GetPlugin(pluginName)
	return ok
}
