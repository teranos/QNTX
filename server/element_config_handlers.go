package server

// Element config handler — server-owned endpoint for reading and writing
// plugin element configuration via attestations.
//
// Convention: subject = "{plugin}-element-{elementID}", predicate = "configured",
// attributes = config JSON. This is the same convention ix-json uses internally.

import (
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// HandleElementConfig handles plugin element configuration via attestations.
//
// Routes:
//
//	GET  /api/element-config?plugin={name}&element_id={id}  - Read element config
//	POST /api/element-config                               - Write element config
func (s *QNTXServer) HandleElementConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetElementConfig(w, r)
	case http.MethodPost:
		s.handleSetElementConfig(w, r)
	}
}

func (s *QNTXServer) handleGetElementConfig(w http.ResponseWriter, r *http.Request) {
	plugin := r.URL.Query().Get("plugin")
	elementID := r.URL.Query().Get("element_id")

	if plugin == "" || elementID == "" {
		writeError(w, http.StatusBadRequest, "plugin and element_id query parameters required")
		return
	}

	store := s.services.ATSStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "attestation store not available")
		return
	}

	subject := fmt.Sprintf("%s-element-%s", plugin, elementID)
	attestations, err := store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Limit:      1,
	})
	if err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to query element config for %s", subject),
			http.StatusInternalServerError)
		return
	}

	if len(attestations) == 0 {
		respond(w, s.logger, http.StatusOK, map[string]any{"config": nil})
		return
	}

	respond(w, s.logger, http.StatusOK, map[string]any{"config": attestations[0].Attributes})
}

func (s *QNTXServer) handleSetElementConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plugin    string         `json:"plugin"`
		ElementID string         `json:"element_id"`
		Config    map[string]any `json:"config"`
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Plugin == "" || req.ElementID == "" {
		writeError(w, http.StatusBadRequest, "plugin and element_id are required")
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

	subject := fmt.Sprintf("%s-element-%s", req.Plugin, req.ElementID)
	cmd := &types.AsCommand{
		Subjects:   []string{subject},
		Predicates: []string{"configured"},
		Contexts:   []string{"_"},
		Attributes: req.Config,
		Source:     fmt.Sprintf("%s-ui", req.Plugin),
	}

	if _, err := store.GenerateAndCreateAttestation(r.Context(), cmd); err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to save element config for %s", subject),
			http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
