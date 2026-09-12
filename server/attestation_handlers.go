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
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/sym"
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
//   - ?since=T      — attestations at or after T ("yesterday", "3 days ago", "2025-01-15")
//   - ?until=T      — attestations at or before T (same expressions as since)
//   - ?on=T         — attestations within the day T falls on (excludes since/until)
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
	asked := time.Now()
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
	admitted, admittedOK := auth.AdmissionFrom(r.Context())
	var narrowAfter []string
	if admittedOK {
		if scope, narrowed := admitted.ReadScope(); narrowed {
			predicates, atTheStore := narrowToScope(filter.Predicates, scope)
			if atTheStore {
				filter.Predicates = predicates
				if len(filter.Predicates) == 0 {
					respond(w, s.logger, http.StatusOK, []any{})
					return
				}
			} else {
				// The limit is the store's, so it counts rows before this
				// narrowing rather than after: a page can come back short.
				narrowAfter = scope
			}
		}
		// Below the ladder a read is what this person wrote and nothing else,
		// unless a READ line said `all`. The actor the node put on their
		// writes is the one asked for.
		if admitted.OwnOnly() {
			filter.Actors = []string{admitted.ActsAs()}
		}
	}

	start, end, errMsg := parseTemporalParams(q.Get("since"), q.Get("until"), q.Get("on"))
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	filter.TimeStart = start
	filter.TimeEnd = end

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

	store, err := s.storeFor(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	attestations, err := store.GetAttestations(filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query attestations", http.StatusInternalServerError)
		return
	}

	if narrowAfter != nil {
		attestations = onlyWhatMayBeRead(narrowAfter, attestations)
	}

	// A query that got slower while answering with the same amount is a
	// different problem from one that got slower because it is answering with
	// more, so both halves are recorded and neither is inferred from the other.
	// Only a query that answered lands here; the refusals above are counted at
	// the door and the store's own failure is already an issue.
	measure.Took(measure.QueryTook, time.Since(asked))
	measure.Sized(measure.QueryReturned, len(attestations))

	respond(w, s.logger, http.StatusOK, attestations)
}

// parseTemporalParams reads the since/until/on query parameters into a time
// range, accepting the same expressions the ax language accepts ("yesterday",
// "3 days ago", "2025-01-15"). on spans the full day its expression falls on,
// matching the ax grammar's on clause; temporal is one clause there, so on
// combined with since or until is refused rather than guessed at.
// Returns an error message, or empty string if valid.
func parseTemporalParams(since, until, on string) (start, end *time.Time, errMsg string) {
	if on != "" && (since != "" || until != "") {
		return nil, nil, fmt.Sprintf("on=%s names a single day and cannot combine with since/until", on)
	}

	if on != "" {
		t, err := parser.ParseTemporalExpression(on)
		if err != nil {
			return nil, nil, fmt.Sprintf("invalid on expression %q: %v", on, err)
		}
		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		endOfDay := startOfDay.Add(24 * time.Hour)
		return &startOfDay, &endOfDay, ""
	}

	if since != "" {
		t, err := parser.ParseTemporalExpression(since)
		if err != nil {
			return nil, nil, fmt.Sprintf("invalid since expression %q: %v", since, err)
		}
		start = t
	}
	if until != "" {
		t, err := parser.ParseTemporalExpression(until)
		if err != nil {
			return nil, nil, fmt.Sprintf("invalid until expression %q: %v", until, err)
		}
		end = t
	}
	return start, end, ""
}

// narrowToScope intersects what was asked for with what is permitted, and says
// whether the store can be told the answer.
//
// An empty request means "everything", which under a scope means everything
// permitted. A namespace in the scope has no literal list — `tag:` is every tag
// there will ever be — so a request for everything under one is read whole and
// narrowed after the store answers, which is what atTheStore false says.
func narrowToScope(asked, scope []string) (predicates []string, atTheStore bool) {
	if len(asked) == 0 {
		if auth.Names(scope) {
			return nil, false
		}
		return slices.Clone(scope), true
	}
	allowed := make([]string, 0, len(asked))
	for _, predicate := range asked {
		if auth.Permits(scope, predicate) {
			allowed = append(allowed, predicate)
		}
	}
	return allowed, true
}

// onlyWhatMayBeRead drops the attestations whose predicate this admission may
// not read. What the store could not be told, it is told here.
func onlyWhatMayBeRead(scope []string, found []*types.As) []*types.As {
	kept := make([]*types.As, 0, len(found))
	for _, as := range found {
		mayRead := len(as.Predicates) > 0
		for _, predicate := range as.Predicates {
			if !auth.Permits(scope, predicate) {
				mayRead = false
				break
			}
		}
		if mayRead {
			kept = append(kept, as)
		}
	}
	return kept
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

	// Validate semantic field sizes
	if err := validateStringArray("subjects", req.Subjects); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateStringArray("predicates", req.Predicates); err != "" {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateNamed(req.Predicates); err != "" {
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

	// A role is an attestation, so granting one is a write like any other and
	// there is no new endpoint. Who may make it is not like any other:
	// MayGrantRoles decides, and nothing else here does.
	//
	// A reach line is the same kind of write: REACH as the subject, the paths,
	// and the roles that reach them. Same writers, same store.
	granting, writesRole := auth.RoleWritten(req.Predicates)
	subject := ""
	if len(req.Subjects) == 1 {
		subject = strings.ToUpper(req.Subjects[0])
	}
	writesReach := subject == reach.Subject
	if writesReach {
		granting, writesRole = reach.Subject, true
		if _, err := reach.ReadLine(req.Subjects, req.Predicates, req.Contexts, req.Actors, time.Now()); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// A WRITE or READ line says what a role may say. ROOT's to write, like a
	// reach line, and kept in the same place.
	writesWords := subject == auth.SubjectWrite || subject == auth.SubjectRead
	if writesWords {
		granting, writesRole = subject, true
		if len(req.Predicates) == 0 || len(req.Contexts) == 0 {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("a %s line names the words as predicates and the roles as contexts", subject))
			return
		}
	}
	if writesRole {
		admitted, ok := auth.AdmissionFrom(r.Context())
		switch {
		case s.authHandler == nil:
			writeError(w, http.StatusForbidden,
				fmt.Sprintf("%s is ROOT's to write, and this node has no login", granting))
			return
		case !ok:
			writeError(w, http.StatusForbidden,
				fmt.Sprintf("%s is ROOT's to write, and this request carries no admission", granting))
			return
		case (writesReach || writesWords) && !s.authHandler.MayGrantRoles(admitted):
			writeError(w, http.StatusForbidden,
				fmt.Sprintf("%s is ROOT's to write, and this admission is %s",
					granting, admitted.LevelName()))
			return
		case !writesReach && !writesWords && !s.mayGrantEvery(admitted, req.Predicates, req.Contexts):
			// `by` is read here: a coordinator grants what a reach line said a
			// coordinator may, in the namespace they act in, and nothing else.
			writeError(w, http.StatusForbidden,
				fmt.Sprintf("%s of %v in %v is not this admission's to write: it is %s holding %v",
					granting, rolesNamed(req.Predicates), req.Contexts, admitted.LevelName(), admitted.Roles()))
			return
		}
	}

	// Every other write is allowed a predicate at a time, so every predicate on
	// the way in is checked rather than the first one. Refusing names the
	// predicate. A grant, a reach line and a word line were decided above by
	// who may write one, not by what a role may say.
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok && !writesRole {
		for _, predicate := range req.Predicates {
			if !admitted.MayWrite(predicate) {
				writeError(w, http.StatusForbidden,
					fmt.Sprintf("%s holding %v may not write %q",
						admitted.LevelName(), admitted.Roles(), predicate))
				return
			}
		}
	}

	// TOKATTEST: each token is its own actor. Its DID leads,
	// because that is the one name here nobody had to be trusted about.
	actors := req.Actors
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok {
		// Two actors can make contradictory claims about the same subject and
		// both are valid (docs/attestation.md), so what a caller names stands.
		// A token signs as its DID; a person holding a role signs as the route
		// they came in by, so a read of their own rows has something to match.
		switch {
		case admitted.ActsAs() != "":
			actors = append([]string{admitted.ActsAs()}, req.Actors...)
		case writesRole:
			// A ROOT session carries no grant, so the line would name no
			// granter — and a grant whose actor is nobody cannot outrank one.
			actors = append([]string{admitted.Identity}, req.Actors...)
		}
	}

	// Roles are kept where the node keeps what it knows about itself, whatever
	// namespace the writer is in. Writing there is not seeing there: this is
	// the one place storeFor does not decide, and it decides nothing else. Who
	// may write one was settled above, by mayGrantEvery and MayGrantRoles.
	var store ats.AttestationStore
	var storeErr error
	if writesRole {
		store, storeErr = s.held.WriteWhatTheNodeKnowsOfItself()
	} else {
		store, storeErr = s.storeFor(r)
	}
	if storeErr != nil {
		writeError(w, http.StatusForbidden, storeErr.Error())
		return
	}

	// A tag exists because somebody attested it, which is what a type is. The
	// tag is attested before the thing tagged with it, so nothing is ever
	// tagged with a tag that does not exist yet.
	s.attestTagsNamed(r, req.Predicates)

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
	if store.AttestationExists(req.ID) {
		respond(w, s.logger, http.StatusOK, map[string]string{"id": req.ID, "status": "exists"})
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
		Actors:     actors,
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
	if hp, ok := store.(highPriorityCreator); ok {
		createErr = hp.CreateAttestationHighPriority(as)
	} else {
		createErr = store.CreateAttestation(as)
	}
	if err := createErr; err != nil {
		writeWrappedError(w, s.logger, err,
			fmt.Sprintf("failed to create attestation %s (subjects: %v, predicates: %v, source: %s)",
				req.ID, req.Subjects, req.Predicates, req.Source),
			http.StatusInternalServerError)
		return
	}

	// What the node holds about who is of what was true until this line. The
	// node is the only writer of one, so this is the whole of keeping up.
	if writesRole && s.authHandler != nil {
		s.authHandler.ForgetRoles()
	}
	// A reach line changes what the node serves, so what it serves is built
	// again from the table and the store, whole. Never patched.
	if writesReach && s.served != nil {
		if unreachable, err := s.served.Reopen(s.answering, s.wrapping(), s.runtime()); err != nil {
			s.logger.Errorw("the reach line is stored and not served; what the node serves is unchanged",
				"id", as.ID, "error", err)
		} else {
			s.logger.Infow("Reach line served", "id", as.ID, "unreachable", unreachable)
		}
	}

	// One per attestation the node took in over the API. The node's own
	// bookkeeping writes — clustering, refusals, plugin sync — are not this
	// number, and the store's own count is the place to go for those.
	measure.Count(measure.AttestationsWritten, 1)

	s.logger.Infow("Attestation created",
		"id", req.ID,
		"subjects", req.Subjects,
		"predicates", req.Predicates,
		"source", req.Source,
		"client", r.RemoteAddr)

	respond(w, s.logger, http.StatusCreated, map[string]string{"id": req.ID, "status": "created"})
}

// attestTagsNamed attests the tags these predicates name, in the universe of
// the writer who named them.
//
// It asks storeFor for that universe rather than being handed a store. The
// role write in handleCreateAttestation lands in the node's own records
// whatever namespace its writer is in, and it is the one write there the
// predicate gate does not run on — so a tag handed that store would be a tag
// in nobody's universe, minted by somebody nothing checked. Asking is what
// makes that unreachable: there is no store to pass in wrongly.
//
// A tag is written by somebody who may write it, so the gate is asked here
// too, for the same reason the store is.
//
// EnsureTypesExist and not EnsureTypes: a tag is a type nobody's code has an
// opinion about, so a colour somebody chose for one is theirs and this leaves
// it alone (ADR-026).
//
// Non-fatal. A tag that was not attested is still a predicate the write may
// carry — what is lost is the tag having a definition, not the tagging.
func (s *QNTXServer) attestTagsNamed(r *http.Request, predicates []string) {
	tags := types.TagsNamed(theseMayBeWritten(r, predicates))
	if len(tags) == 0 {
		return
	}

	store, err := s.storeFor(r)
	if err != nil {
		s.logger.Warnw(sym.Type+" A tag was named by a writer who reaches no universe",
			"tags", tags, "error", err)
		return
	}

	says := ats.TypesSaid(store, tags...)
	if err := types.EnsureTypesExist(store, says, "tagging", types.TagDefs(tags)...); err != nil {
		s.logger.Warnw(sym.Type+" A tag was written without a definition",
			"tags", tags, "error", err)
	}
}

// theseMayBeWritten is the predicates this request's admission may write.
//
// A request carrying no admission is the node asking itself, which storeFor
// answers with the namespace it serves; what it may write is what it asked to.
func theseMayBeWritten(r *http.Request, predicates []string) []string {
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok {
		return predicates
	}
	var written []string
	for _, predicate := range predicates {
		if admitted.MayWrite(predicate) {
			written = append(written, predicate)
		}
	}
	return written
}

// validateNamed refuses a predicate that names a namespace rather than a thing.
//
// A word ending in the namespace marker is every predicate under it: `tag:` is
// every tag there will ever be. That is a word a WRITE line says, and an
// attestation cannot make a claim about all of them at once — what it carries
// is `tag:ci-runner`, one tag, the one it means.
//
// Trimmed first, so a tag named by a space is refused with the tag named by
// nothing: neither is a tag anybody named.
func validateNamed(predicates []string) string {
	for _, predicate := range predicates {
		if strings.HasSuffix(strings.TrimSpace(predicate), auth.Namespace) {
			return fmt.Sprintf("the predicate %q names every predicate under it rather than one of them", predicate)
		}
	}
	return ""
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
