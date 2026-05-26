package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// WatcherHandler serves watcher CRUD and queue stats endpoints.
type WatcherHandler struct {
	engine *watcher.Engine
	logger *zap.SugaredLogger
}

// NewWatcherHandler creates a handler for watcher endpoints.
func NewWatcherHandler(engine *watcher.Engine, logger *zap.SugaredLogger) *WatcherHandler {
	return &WatcherHandler{engine: engine, logger: logger}
}

// HandleWatchers handles watcher CRUD operations
// Routes:
//
//	GET    /api/watchers       - List all watchers
//	POST   /api/watchers       - Create a new watcher
//	GET    /api/watchers/{id}  - Get a watcher by ID
//	PUT    /api/watchers/{id}  - Update a watcher
//	DELETE /api/watchers/{id}  - Delete a watcher
func (h *WatcherHandler) HandleWatchers(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		writeRichError(w, h.logger, errors.New("watcher engine not initialized"), http.StatusServiceUnavailable)
		return
	}

	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/watchers")
	path = strings.TrimPrefix(path, "/")
	watcherID := path

	switch r.Method {
	case http.MethodGet:
		if watcherID == "" {
			h.handleListWatchers(w, r)
		} else {
			h.handleGetWatcher(w, r, watcherID)
		}
	case http.MethodPost:
		h.handleCreateWatcher(w, r)
	case http.MethodPut:
		if watcherID == "" {
			http.Error(w, "Watcher ID required", http.StatusBadRequest)
			return
		}
		h.handleUpdateWatcher(w, r, watcherID)
	case http.MethodDelete:
		if watcherID == "" {
			http.Error(w, "Watcher ID required", http.StatusBadRequest)
			return
		}
		h.handleDeleteWatcher(w, r, watcherID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *WatcherHandler) handleListWatchers(w http.ResponseWriter, r *http.Request) {
	enabledOnly := r.URL.Query().Get("enabled") == "true"

	watchers, err := h.engine.GetStore().List(r.Context(), enabledOnly)
	if err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "failed to list watchers"), http.StatusInternalServerError)
		return
	}

	response := make([]WatcherResponse, len(watchers))
	for i, watcher := range watchers {
		response[i] = watcherToResponse(watcher)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *WatcherHandler) handleGetWatcher(w http.ResponseWriter, r *http.Request, id string) {
	watcher, err := h.engine.GetStore().Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeRichError(w, h.logger, err, http.StatusNotFound)
		} else {
			writeRichError(w, h.logger, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(watcherToResponse(watcher))
}

func (h *WatcherHandler) handleCreateWatcher(w http.ResponseWriter, r *http.Request) {
	var req WatcherCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ID == "" {
		writeRichError(w, h.logger, errors.New("id is required"), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		writeRichError(w, h.logger, errors.New("name is required"), http.StatusBadRequest)
		return
	}
	if req.ActionType == "" {
		writeRichError(w, h.logger, errors.New("action_type is required"), http.StatusBadRequest)
		return
	}
	switch req.ActionType {
	case "python", "webhook":
		if req.ActionData == "" {
			writeRichError(w, h.logger, errors.New("action_data is required for python/webhook watchers"), http.StatusBadRequest)
			return
		}
	case "plugin_execute", "glyph_execute":
		if req.ActionData == "" {
			writeRichError(w, h.logger, errors.Newf("action_data is required for %s watchers", req.ActionType), http.StatusBadRequest)
			return
		}
	case "semantic_match":
		if req.SemanticQuery == "" {
			writeRichError(w, h.logger, errors.New("semantic_query is required for semantic_match watchers"), http.StatusBadRequest)
			return
		}
	default:
		writeRichError(w, h.logger, errors.Newf("invalid action_type: %s", req.ActionType), http.StatusBadRequest)
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
			writeRichError(w, h.logger, errors.Wrap(err, "invalid time_start format (use RFC3339)"), http.StatusBadRequest)
			return
		}
		watcher.Filter.TimeStart = &t
	}
	if req.TimeEnd != "" {
		t, err := time.Parse(time.RFC3339, req.TimeEnd)
		if err != nil {
			writeRichError(w, h.logger, errors.Wrap(err, "invalid time_end format (use RFC3339)"), http.StatusBadRequest)
			return
		}
		watcher.Filter.TimeEnd = &t
	}

	// Create watcher
	if err := h.engine.GetStore().Create(r.Context(), watcher); err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "failed to create watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := h.engine.ReloadWatchers(); err != nil {
		h.logger.Warnw("Failed to reload watchers after create", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(watcherToResponse(watcher))
}

func (h *WatcherHandler) handleUpdateWatcher(w http.ResponseWriter, r *http.Request, id string) {
	// Get existing watcher
	existing, err := h.engine.GetStore().Get(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeRichError(w, h.logger, err, http.StatusNotFound)
		} else {
			writeRichError(w, h.logger, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	var req WatcherCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
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
			writeRichError(w, h.logger, errors.Wrap(err, "invalid time_start format"), http.StatusBadRequest)
			return
		}
		existing.Filter.TimeStart = &t
	}
	if req.TimeEnd != "" {
		t, err := time.Parse(time.RFC3339, req.TimeEnd)
		if err != nil {
			writeRichError(w, h.logger, errors.Wrap(err, "invalid time_end format"), http.StatusBadRequest)
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
	if err := h.engine.GetStore().Update(r.Context(), existing); err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "failed to update watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := h.engine.ReloadWatchers(); err != nil {
		h.logger.Warnw("Failed to reload watchers after update", "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(watcherToResponse(existing))
}

func (h *WatcherHandler) handleDeleteWatcher(w http.ResponseWriter, r *http.Request, id string) {
	// Verify watcher exists
	if _, err := h.engine.GetStore().Get(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeRichError(w, h.logger, err, http.StatusNotFound)
		} else {
			writeRichError(w, h.logger, errors.Wrap(err, "failed to get watcher"), http.StatusInternalServerError)
		}
		return
	}

	// Delete
	if err := h.engine.GetStore().Delete(r.Context(), id); err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "failed to delete watcher"), http.StatusInternalServerError)
		return
	}

	// Reload watchers in engine
	if err := h.engine.ReloadWatchers(); err != nil {
		h.logger.Warnw("Failed to reload watchers after delete", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleWatcherQueueStats returns execution queue statistics
func (h *WatcherHandler) HandleWatcherQueueStats(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		writeRichError(w, h.logger, errors.New("watcher engine not initialized"), http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats, err := h.engine.GetQueueStore().Stats()
	if err != nil {
		writeRichError(w, h.logger, errors.Wrap(err, "failed to get queue stats"), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
