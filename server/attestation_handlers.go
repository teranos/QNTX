package server

// Attestation HTTP handlers — query and create attestations.

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
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

// HandleAttestations routes GET (query) and POST (create) for /api/attestations.
// GET returns attestations matching optional filters (JSON array).
// Query parameters:
//   - ?subject=x    — filter by subject(s), comma-separated
//   - ?predicate=y  — filter by predicate(s), comma-separated
//   - ?context=z    — filter by context(s), comma-separated
//   - ?actor=a      — filter by actor(s), comma-separated
//   - ?source=s     — filter by source (exact match, e.g. "cli", "distill")
//   - ?limit=N      — max results (default 100, max 1000)
//
// POST creates an attestation (idempotent, returns 200 if already exists).
func (s *QNTXServer) HandleAttestations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetAttestations(w, r)
	case http.MethodPost:
		s.handleCreateAttestation(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleGetAttestations queries attestations with optional filters.
// GET /api/attestations?subject=x&predicate=y&context=z&actor=a&limit=100
// Multiple values for the same param use comma separation: ?predicate=a,b
func (s *QNTXServer) handleGetAttestations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := ats.AttestationFilter{
		Subjects:   splitParam(q.Get("subject")),
		Predicates: splitParam(q.Get("predicate")),
		Contexts:   splitParam(q.Get("context")),
		Actors:     splitParam(q.Get("actor")),
		Source:     q.Get("source"),
		Limit:      100, // default
	}

	// Read scope narrows the query rather than refusing it. A token scoped to
	// one predicate that asks for everything gets its one predicate — asking
	// broadly is not an attempt to overreach, and a filter is the honest answer.
	if caller, ok := auth.CallerFrom(r.Context()); ok && caller.Grant != nil && !caller.Grant.Unrestricted() {
		filter.Predicates = narrowToScope(filter.Predicates, caller.Grant.ScopeRead)
		if len(filter.Predicates) == 0 {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid limit: %s", v))
			return
		}
		if n > 1000 {
			n = 1000
		}
		filter.Limit = n
	}

	attestations, err := s.atsStore.GetAttestations(filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query attestations", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, attestations)
}

// narrowToScope intersects what was asked for with what is permitted. An empty
// request means "everything", which under a scope means everything permitted.
func narrowToScope(asked, scope []string) []string {
	if len(asked) == 0 {
		return slices.Clone(scope)
	}
	allowed := make([]string, 0, len(asked))
	for _, predicate := range asked {
		if slices.Contains(scope, predicate) {
			allowed = append(allowed, predicate)
		}
	}
	return allowed
}

// splitParam splits a comma-separated query parameter into a string slice.
// Returns nil for empty input.
func splitParam(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// handleCreateAttestation accepts a browser-created attestation and stores it server-side.
// POST /api/attestations — idempotent (returns 200 if already exists).
func (s *QNTXServer) handleCreateAttestation(w http.ResponseWriter, r *http.Request) {

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

	// A token is allowed a predicate at a time, so every predicate on the way in
	// is checked rather than the first one. Refusing names the predicate: a
	// scope failure the caller cannot see is one they cannot fix.
	if caller, ok := auth.CallerFrom(r.Context()); ok {
		for _, predicate := range req.Predicates {
			if !caller.MayWrite(predicate) {
				writeError(w, http.StatusForbidden,
					fmt.Sprintf("this token may not write predicate %q; its write scope is %v",
						predicate, caller.Grant.ScopeWrite))
				return
			}
		}
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

	// Auto-generate vanity ASID when client omits ID
	if req.ID == "" {
		subject := req.Subjects[0]
		predicate := req.Predicates[0]
		context := "_"
		if len(req.Contexts) > 0 {
			context = req.Contexts[0]
		}
		checkExists := func(asid string) bool {
			return s.atsStore.AttestationExists(asid)
		}
		generated, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, context, checkExists)
		if err != nil {
			writeWrappedError(w, s.logger, err,
				fmt.Sprintf("failed to generate ASID for subjects %v", req.Subjects),
				http.StatusInternalServerError)
			return
		}
		req.ID = generated
	}

	// Idempotent: if already exists, return success
	if s.atsStore.AttestationExists(req.ID) {
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

	// Use high priority so POST jumps ahead of queued plugin writes.
	var createErr error
	type highPriorityCreator interface {
		CreateAttestationHighPriority(as *types.As) error
	}
	if hp, ok := s.atsStore.(highPriorityCreator); ok {
		createErr = hp.CreateAttestationHighPriority(as)
	} else {
		createErr = s.atsStore.CreateAttestation(as)
	}
	if err := createErr; err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to create attestation %s (subjects: %v, predicates: %v, source: %s)",
				req.ID, req.Subjects, req.Predicates, req.Source),
			http.StatusInternalServerError)
		return
	}

	s.logger.Infow("Attestation created",
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
