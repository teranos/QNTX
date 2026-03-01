package server

// Glyph config handler — server-owned endpoint for reading and writing
// plugin glyph configuration via attestations.
//
// Convention: subject = "{plugin}-glyph-{glyphID}", predicate = "configured",
// attributes = config JSON. This is the same convention ix-json uses internally.

import (
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// HandleGlyphConfig handles plugin glyph configuration via attestations.
//
// Routes:
//
//	GET  /api/glyph-config?plugin={name}&glyph_id={id}  - Read glyph config
//	POST /api/glyph-config                               - Write glyph config
func (s *QNTXServer) HandleGlyphConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetGlyphConfig(w, r)
	case http.MethodPost:
		s.handleSetGlyphConfig(w, r)
	}
}

func (s *QNTXServer) handleGetGlyphConfig(w http.ResponseWriter, r *http.Request) {
	plugin := r.URL.Query().Get("plugin")
	glyphID := r.URL.Query().Get("glyph_id")

	if plugin == "" || glyphID == "" {
		writeError(w, http.StatusBadRequest, "plugin and glyph_id query parameters required")
		return
	}

	store := s.services.ATSStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "attestation store not available")
		return
	}

	subject := fmt.Sprintf("%s-glyph-%s", plugin, glyphID)
	attestations, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Limit:      1,
	})
	if err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to query glyph config for %s", subject),
			http.StatusInternalServerError)
		return
	}

	if len(attestations) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"config": nil})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"config": attestations[0].Attributes})
}

func (s *QNTXServer) handleSetGlyphConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plugin  string                 `json:"plugin"`
		GlyphID string                 `json:"glyph_id"`
		Config  map[string]any `json:"config"`
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Plugin == "" || req.GlyphID == "" {
		writeError(w, http.StatusBadRequest, "plugin and glyph_id are required")
		return
	}

	if req.Config == nil {
		writeError(w, http.StatusBadRequest, "config is required")
		return
	}

	store := s.services.ATSStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "attestation store not available")
		return
	}

	subject := fmt.Sprintf("%s-glyph-%s", req.Plugin, req.GlyphID)
	cmd := &types.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Contexts:   []string{"_"},
		Attributes: req.Config,
		Source:     fmt.Sprintf("%s-ui", req.Plugin),
	}

	if _, err := store.GenerateAndCreateAttestation(r.Context(), cmd); err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to save glyph config for %s", subject),
			http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
