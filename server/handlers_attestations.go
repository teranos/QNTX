package server

// Attestation HTTP handler — accepts attestations created offline in the browser
// and persists them to the server-side SQLite store.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	id "github.com/teranos/vanity-id"
)

// HandleCreateAttestation accepts a browser-created attestation and stores it server-side.
// POST /api/attestations — idempotent (returns 200 if already exists).
func (s *QNTXServer) HandleCreateAttestation(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		ID         string   `json:"id"`
		Subjects   []string `json:"subjects"`
		Predicates []string `json:"predicates"`
		Contexts   []string `json:"contexts"`
		Actors     []string `json:"actors"`
		Timestamp  int64    `json:"timestamp"`
		Source     string   `json:"source"`
		Attributes string   `json:"attributes"`
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	// Validate required fields
	if len(req.Subjects) == 0 {
		writeError(w, http.StatusBadRequest, "subjects must not be empty")
		return
	}
	if len(req.Predicates) == 0 {
		writeError(w, http.StatusBadRequest, "predicates must not be empty")
		return
	}

	store := storage.NewBoundedStore(s.db, s.logger.Named("attestation-sync"))

	// Auto-generate vanity ASID when client omits ID
	if req.ID == "" {
		subject := req.Subjects[0]
		predicate := req.Predicates[0]
		context := "_"
		if len(req.Contexts) > 0 {
			context = req.Contexts[0]
		}
		checkExists := func(asid string) bool {
			return store.AttestationExists(asid)
		}
		generated, err := id.GenerateASIDWithVanityAndRetry(subject, predicate, context, "", checkExists)
		if err != nil {
			writeWrappedError(w, s.logger, err,
				fmt.Sprintf("failed to generate ASID for subjects %v", req.Subjects),
				http.StatusInternalServerError)
			return
		}
		req.ID = generated
	}

	// Idempotent: if already exists, return success
	if store.AttestationExists(req.ID) {
		writeJSON(w, http.StatusOK, map[string]string{"id": req.ID, "status": "exists"})
		return
	}

	// Parse attributes JSON string to map
	attrs := parseAttributesJSON(req.Attributes)

	ts := time.Unix(req.Timestamp, 0)
	if req.Timestamp == 0 {
		ts = time.Now()
	}

	as := &types.As{
		ID:         req.ID,
		Subjects:   req.Subjects,
		Predicates: req.Predicates,
		Contexts:   req.Contexts,
		Actors:     req.Actors,
		Timestamp:  ts,
		Source:     req.Source,
		Attributes: attrs,
		CreatedAt:  time.Now(),
	}

	if err := store.CreateAttestation(as); err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to create attestation %s (subjects: %v, predicates: %v, source: %s)",
				req.ID, req.Subjects, req.Predicates, req.Source),
			http.StatusInternalServerError)
		return
	}

	s.logger.Infow("Attestation synced from browser",
		"id", req.ID,
		"subjects", req.Subjects,
		"predicates", req.Predicates,
		"source", req.Source,
		"client", r.RemoteAddr)

	writeJSON(w, http.StatusCreated, map[string]string{"id": req.ID, "status": "created"})
}

// parseAttributesJSON safely parses a JSON string into a map.
// Returns nil on empty/invalid input (attributes are optional metadata).
func parseAttributesJSON(raw string) map[string]any {
	if raw == "" || raw == "{}" || raw == "null" {
		return nil
	}

	var attrs map[string]any
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return nil
	}
	return attrs
}
