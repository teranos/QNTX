package server

// Attestation HTTP handler — accepts attestations created offline in the browser
// and persists them to the server-side SQLite store.

import (
	"fmt"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	id "github.com/teranos/vanity-id"
)

// Attestation size limits.
const (
	// maxAttestationBody matches the WebSocket message limit (client.go:maxMessageSize).
	// An attestation that can't survive the WebSocket shouldn't enter the store.
	// TODO: Make configurable via am.toml when image-carrying attestations ship.
	maxAttestationBody = 10 * 1024 * 1024 // 10 MB

	// Semantic field limits — these fields are short identifiers, not free text.
	maxArrayElements = 100
	maxStringLength  = 1000
)

// HandleCreateAttestation accepts a browser-created attestation and stores it server-side.
// POST /api/attestations — idempotent (returns 200 if already exists).
func (s *QNTXServer) HandleCreateAttestation(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Cap request body to prevent unbounded memory allocation.
	r.Body = http.MaxBytesReader(w, r.Body, maxAttestationBody)

	var req struct {
		ID         string                 `json:"id"`
		Subjects   []string               `json:"subjects"`
		Predicates []string               `json:"predicates"`
		Contexts   []string               `json:"contexts"`
		Actors     []string               `json:"actors"`
		Timestamp  int64                  `json:"timestamp"`
		Source     string                 `json:"source"`
		Attributes map[string]interface{} `json:"attributes"`
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

	// Validate semantic field sizes
	if err := validateStringArray("subjects", req.Subjects); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateStringArray("predicates", req.Predicates); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateStringArray("contexts", req.Contexts); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateStringArray("actors", req.Actors); err != "" {
		writeError(w, http.StatusBadRequest, err)
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
		Attributes: req.Attributes,
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

// validateStringArray checks that an array doesn't exceed element count or string length limits.
// Returns an error message, or empty string if valid.
func validateStringArray(field string, values []string) string {
	if len(values) > maxArrayElements {
		return fmt.Sprintf("%s: too many elements (%d, max %d)", field, len(values), maxArrayElements)
	}
	for _, v := range values {
		if len(v) > maxStringLength {
			return fmt.Sprintf("%s: element too long (%d bytes, max %d)", field, len(v), maxStringLength)
		}
	}
	return ""
}
