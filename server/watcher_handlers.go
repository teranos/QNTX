package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/errors"
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
	MaxFiresPerMinute int      `json:"max_fires_per_minute,omitempty"`
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
	MaxFiresPerMinute int      `json:"max_fires_per_minute"`
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
	case "semantic_match":
		if req.SemanticQuery == "" {
			s.writeRichError(w, errors.New("semantic_query is required for semantic_match watchers"), http.StatusBadRequest)
			return
		}
	default:
		s.writeRichError(w, errors.Newf("invalid action_type: %s (must be 'python', 'webhook', or 'semantic_match')", req.ActionType), http.StatusBadRequest)
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
		MaxFiresPerMinute: 105, // Default
		Enabled:           true,
	}

	if req.MaxFiresPerMinute > 0 {
		watcher.MaxFiresPerMinute = req.MaxFiresPerMinute
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
	if req.MaxFiresPerMinute > 0 {
		existing.MaxFiresPerMinute = req.MaxFiresPerMinute
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
		MaxFiresPerMinute: w.MaxFiresPerMinute,
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

	s.watcherEngine = watcher.NewEngine(s.db, apiBaseURL, s.logger)

	// Set broadcast callback for live results
	s.watcherEngine.SetBroadcastCallback(s.broadcastWatcherMatch)

	// Set glyph fired callback for meld-triggered execution feedback
	s.watcherEngine.SetGlyphFiredCallback(s.broadcastGlyphFired)

	// Wire embedding service for semantic matching (optional — nil when embeddings unavailable)
	// Note: embeddingService may be nil here if SetupEmbeddingService() hasn't run yet.
	// In that case, init.go reconnects after embedding init.
	if s.embeddingService != nil {
		s.watcherEngine.SetEmbeddingService(&watcherEmbeddingAdapter{svc: s.embeddingService})
		if s.embeddingStore != nil {
			s.watcherEngine.SetEmbeddingSearcher(&watcherSearchAdapter{store: s.embeddingStore})
		}
	}

	// Register as global observer (notified by SQLStore on all attestation creations)
	storage.RegisterObserver(s.watcherEngine)

	// Start the engine
	if err := s.watcherEngine.Start(); err != nil {
		return errors.Wrap(err, "failed to start watcher engine")
	}

	s.logger.Info("Watcher engine initialized")
	return nil
}

// watcherEmbeddingAdapter adapts the server's embedding service (which returns
// *embeddings.EmbeddingResult) to the watcher engine's simpler interface.
type watcherEmbeddingAdapter struct {
	svc interface {
		GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
	}
}

func (a *watcherEmbeddingAdapter) GenerateEmbedding(text string) ([]float32, error) {
	result, err := a.svc.GenerateEmbedding(text)
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
	results, err := a.store.SemanticSearch(queryEmbedding, limit, threshold, clusterID)
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
