package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/errors"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
)

// CanvasHandler handles HTTP requests for canvas state
type CanvasHandler struct {
	store *glyphstorage.CanvasStore
}

// NewCanvasHandler creates a new canvas handler
func NewCanvasHandler(store *glyphstorage.CanvasStore) *CanvasHandler {
	return &CanvasHandler{
		store: store,
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
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, comp)
}

func (h *CanvasHandler) handleDeleteComposition(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.DeleteComposition(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Helper methods ===

func (h *CanvasHandler) writeJSON(w http.ResponseWriter, data interface{}) {
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
