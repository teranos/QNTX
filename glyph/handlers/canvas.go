package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/errors"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/sym"
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
//   GET    /api/canvas/glyphs       - List all glyphs
//   POST   /api/canvas/glyphs       - Create/update a glyph
//   GET    /api/canvas/glyphs/{id}  - Get a glyph by ID
//   DELETE /api/canvas/glyphs/{id}  - Delete a glyph
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
//   GET    /api/canvas/compositions       - List all compositions
//   POST   /api/canvas/compositions       - Create/update a composition
//   GET    /api/canvas/compositions/{id}  - Get a composition by ID
//   DELETE /api/canvas/compositions/{id}  - Delete a composition
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
		if strings.Contains(err.Error(), "not found") {
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
		if strings.Contains(err.Error(), "not found") {
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
		if strings.Contains(err.Error(), "not found") {
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
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			// TODO(#431): Queue deletion for retry when offline
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Subscription compilation ===

// compileSubscriptions converts a composition's right-direction edges into watcher subscriptions.
// AX source edges use the AX glyph's query filter. Producer (py/prompt) source edges filter on actor.
func (h *CanvasHandler) compileSubscriptions(ctx context.Context, comp *glyphstorage.CanvasComposition) error {
	store := h.watcherEngine.GetStore()

	// Clean up existing meld-edge watchers for this composition
	prefix := fmt.Sprintf("meld-edge-%s-", comp.ID)
	if _, err := store.DeleteByPrefix(ctx, prefix); err != nil {
		return errors.Wrapf(err, "failed to clean up old subscriptions for composition %s", comp.ID)
	}

	var created int
	for _, edge := range comp.Edges {
		if edge.Direction != "right" {
			continue
		}

		// Resolve source glyph type
		sourceGlyph, err := h.store.GetGlyph(ctx, edge.From)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve source glyph: %v", edge.From, edge.To, err)
			continue
		}

		// Resolve target glyph type
		targetGlyph, err := h.store.GetGlyph(ctx, edge.To)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve target glyph: %v", edge.From, edge.To, err)
			continue
		}

		// Target must be an executable glyph type
		targetType := glyphSymbolToType(targetGlyph.Symbol)
		if targetType != "py" && targetType != "prompt" {
			continue
		}

		watcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, edge.From, edge.To)
		actionData, err := json.Marshal(map[string]string{
			"target_glyph_id":   edge.To,
			"target_glyph_type": targetType,
			"composition_id":    comp.ID,
			"source_glyph_id":   edge.From,
		})
		if err != nil {
			return errors.Wrap(err, "failed to marshal action data")
		}

		w := &storage.Watcher{
			ID:                watcherID,
			Name:              fmt.Sprintf("Meld: %s → %s", truncate(edge.From, 8), truncate(edge.To, 8)),
			ActionType:        storage.ActionTypeGlyphExecute,
			ActionData:        string(actionData),
			MaxFiresPerMinute: 60,
			Enabled:           true,
		}

		// Set filter based on source glyph type
		sourceType := glyphSymbolToType(sourceGlyph.Symbol)
		switch sourceType {
		case "ax":
			// AX source: reuse the AX glyph's query from its existing watcher
			axWatcherID := fmt.Sprintf("ax-glyph-%s", edge.From)
			axWatcher, err := store.Get(ctx, axWatcherID)
			if err != nil {
				h.logWarn("Skipping AX edge %s→%s: no watcher found for %s: %v", edge.From, edge.To, axWatcherID, err)
				continue
			}
			w.AxQuery = axWatcher.AxQuery
		case "py", "prompt":
			// Producer source: filter on attestations created by the upstream glyph
			w.Filter.Actors = []string{fmt.Sprintf("glyph:%s", edge.From)}
		default:
			h.logWarn("Skipping edge %s→%s: unsupported source type %s", edge.From, edge.To, sourceType)
			continue
		}

		if err := store.Create(ctx, w); err != nil {
			return errors.Wrapf(err, "failed to create subscription for edge %s→%s", edge.From, edge.To)
		}
		created++
	}

	if created > 0 {
		if err := h.watcherEngine.ReloadWatchers(); err != nil {
			return errors.Wrap(err, "failed to reload watchers after subscription compilation")
		}
		h.logInfo("Compiled %d subscriptions for composition %s", created, comp.ID)
	}

	return nil
}

// glyphSymbolToType maps glyph symbol to short type name for subscription logic.
// Symbols come from the sym package or are stored as literal strings (e.g. "py").
func glyphSymbolToType(symbol string) string {
	switch symbol {
	case "py":
		return "py"
	case sym.AX: // ⋈
		return "ax"
	case sym.SO: // ⟶ — prompt glyph uses SO symbol
		return "prompt"
	default:
		return symbol
	}
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
