package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/errors"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"go.uber.org/zap"
)

// CanvasHandler handles HTTP requests for canvas state
type CanvasHandler struct {
	store         *glyphstorage.CanvasStore
	watcherEngine *watcher.Engine
	logger        *zap.SugaredLogger
}

// NewCanvasHandler creates a new canvas handler
func NewCanvasHandler(store *glyphstorage.CanvasStore, opts ...CanvasHandlerOption) *CanvasHandler {
	h := &CanvasHandler{
		store: store,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// CanvasHandlerOption configures optional dependencies for the canvas handler
type CanvasHandlerOption func(*CanvasHandler)

// WithWatcherEngine enables meld edge subscription compilation
func WithWatcherEngine(engine *watcher.Engine, logger *zap.SugaredLogger) CanvasHandlerOption {
	return func(h *CanvasHandler) {
		h.watcherEngine = engine
		h.logger = logger
	}
}

// HandleGlyphs handles glyph CRUD operations
// Routes:
//
//	GET    /api/canvas/glyphs       - List all glyphs
//	POST   /api/canvas/glyphs       - Create/update a glyph
//	GET    /api/canvas/glyphs/{id}  - Get a glyph by ID
//	DELETE /api/canvas/glyphs/{id}  - Delete a glyph
func (h *CanvasHandler) HandleGlyphs(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/glyphs")
	path = strings.TrimPrefix(path, "/")
	glyphID := path

	switch r.Method {
	case http.MethodGet:
		if glyphID == "" {
			h.handleListGlyphs(w, r)
		} else {
			h.handleGetGlyph(w, r, glyphID)
		}
	case http.MethodPost:
		h.handleUpsertGlyph(w, r)
	case http.MethodDelete:
		if glyphID == "" {
			h.writeError(w, errors.New("glyph ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleDeleteGlyph(w, r, glyphID)
	default:
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
	}
}

// HandleCompositions handles composition CRUD operations
// Routes:
//
//	GET    /api/canvas/compositions       - List all compositions
//	POST   /api/canvas/compositions       - Create/update a composition
//	GET    /api/canvas/compositions/{id}  - Get a composition by ID
//	DELETE /api/canvas/compositions/{id}  - Delete a composition
func (h *CanvasHandler) HandleCompositions(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/compositions")
	path = strings.TrimPrefix(path, "/")
	compID := path

	switch r.Method {
	case http.MethodGet:
		if compID == "" {
			h.handleListCompositions(w, r)
		} else {
			h.handleGetComposition(w, r, compID)
		}
	case http.MethodPost:
		h.handleUpsertComposition(w, r)
	case http.MethodDelete:
		if compID == "" {
			h.writeError(w, errors.New("composition ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleDeleteComposition(w, r, compID)
	default:
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
	}
}

// === Glyph handlers ===

func (h *CanvasHandler) handleListGlyphs(w http.ResponseWriter, r *http.Request) {
	glyphs, err := h.store.ListGlyphs(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, glyphs)
}

func (h *CanvasHandler) handleGetGlyph(w http.ResponseWriter, r *http.Request, id string) {
	glyph, err := h.store.GetGlyph(r.Context(), id)
	if err != nil {
		if errors.Is(err, glyphstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	h.writeJSON(w, glyph)
}

func (h *CanvasHandler) handleUpsertGlyph(w http.ResponseWriter, r *http.Request) {
	var glyph glyphstorage.CanvasGlyph
	if err := json.NewDecoder(r.Body).Decode(&glyph); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if err := h.store.UpsertGlyph(r.Context(), &glyph); err != nil {
		// TODO(#431): Implement offline queue support for failed canvas operations
		// When storage fails (network issues, database locked), queue operation
		// for retry instead of immediately failing the request
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, glyph)
}

func (h *CanvasHandler) handleDeleteGlyph(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.DeleteGlyph(r.Context(), id); err != nil {
		if errors.Is(err, glyphstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			// TODO(#431): Queue deletion for retry when offline
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Composition handlers ===

func (h *CanvasHandler) handleListCompositions(w http.ResponseWriter, r *http.Request) {
	comps, err := h.store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, comps)
}

func (h *CanvasHandler) handleGetComposition(w http.ResponseWriter, r *http.Request, id string) {
	comp, err := h.store.GetComposition(r.Context(), id)
	if err != nil {
		if errors.Is(err, glyphstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	h.writeJSON(w, comp)
}

func (h *CanvasHandler) handleUpsertComposition(w http.ResponseWriter, r *http.Request) {
	var comp glyphstorage.CanvasComposition
	if err := json.NewDecoder(r.Body).Decode(&comp); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if err := h.store.UpsertComposition(r.Context(), &comp); err != nil {
		// TODO(#431): Queue operation for retry when offline
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	// Compile meld edges into watcher subscriptions
	if h.watcherEngine != nil {
		if err := h.compileSubscriptions(r.Context(), &comp); err != nil {
			h.logWarn("Failed to compile subscriptions for composition %s: %v", comp.ID, err)
			// Non-fatal: composition is stored, subscriptions can be retried
		}
	}

	h.writeJSON(w, comp)
}

func (h *CanvasHandler) handleDeleteComposition(w http.ResponseWriter, r *http.Request, id string) {
	// Re-enable downstream SE watchers that were disabled by SE→SE meld edges
	if h.watcherEngine != nil {
		h.reEnableDownstreamSEWatchers(r.Context(), id)
	}

	// Remove meld edge subscriptions before deleting composition
	if h.watcherEngine != nil {
		prefix := fmt.Sprintf("meld-edge-%s-", id)
		if n, err := h.watcherEngine.GetStore().DeleteByPrefix(r.Context(), prefix); err != nil {
			h.logWarn("Failed to delete meld edge watchers for composition %s: %v", id, err)
		} else if n > 0 {
			h.logInfo("Deleted %d meld edge watchers for composition %s", n, id)
			if err := h.watcherEngine.ReloadWatchers(); err != nil {
				h.logWarn("Failed to reload watchers after composition %s delete: %v", id, err)
			}
		}
	}

	// Cascade delete edge cursors
	if h.watcherEngine != nil {
		if _, err := h.watcherEngine.DB().ExecContext(r.Context(),
			"DELETE FROM composition_edge_cursors WHERE composition_id = ?", id); err != nil {
			h.logWarn("Failed to delete edge cursors for composition %s: %v", id, err)
		}
	}

	if err := h.store.DeleteComposition(r.Context(), id); err != nil {
		if errors.Is(err, glyphstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			// TODO(#431): Queue deletion for retry when offline
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleMinimizedWindows handles minimized window CRUD operations
// Routes:
//
//	GET    /api/canvas/minimized-windows       - List all minimized windows
//	POST   /api/canvas/minimized-windows       - Add a minimized window
//	DELETE /api/canvas/minimized-windows/{id}  - Remove a minimized window
func (h *CanvasHandler) HandleMinimizedWindows(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/minimized-windows")
	path = strings.TrimPrefix(path, "/")
	glyphID := path

	switch r.Method {
	case http.MethodGet:
		h.handleListMinimizedWindows(w, r)
	case http.MethodPost:
		h.handleAddMinimizedWindow(w, r)
	case http.MethodDelete:
		if glyphID == "" {
			h.writeError(w, errors.New("glyph ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleRemoveMinimizedWindow(w, r, glyphID)
	default:
		h.writeError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
	}
}

func (h *CanvasHandler) handleListMinimizedWindows(w http.ResponseWriter, r *http.Request) {
	windows, err := h.store.ListMinimizedWindows(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, windows)
}

func (h *CanvasHandler) handleAddMinimizedWindow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GlyphID string `json:"glyph_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if body.GlyphID == "" {
		h.writeError(w, errors.New("glyph_id is required"), http.StatusBadRequest)
		return
	}

	if err := h.store.AddMinimizedWindow(r.Context(), body.GlyphID); err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *CanvasHandler) handleRemoveMinimizedWindow(w http.ResponseWriter, r *http.Request, glyphID string) {
	if err := h.store.RemoveMinimizedWindow(r.Context(), glyphID); err != nil {
		if errors.Is(err, glyphstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Helper methods ===

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (h *CanvasHandler) logInfo(format string, args ...any) {
	if h.logger != nil {
		h.logger.Infof(format, args...)
	}
}

func (h *CanvasHandler) logWarn(format string, args ...any) {
	if h.logger != nil {
		h.logger.Warnf(format, args...)
	}
}

func (h *CanvasHandler) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *CanvasHandler) writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": err.Error(),
	})
}
