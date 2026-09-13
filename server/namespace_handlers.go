package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// maxNamespaceBodyBytes bounds the create body. The whole of it is one name,
// and a name is one path segment.
const maxNamespaceBodyBytes = 8 << 10

// createNamespaceRequest is what SUPER supplies: a name. Ownership is not the
// request's to state — the node signs it and records who asked.
type createNamespaceRequest struct {
	Name string `json:"name"`
}

// listNamespacesResponse names the count so an empty list and a backend that
// keeps none are visibly different answers.
type listNamespacesResponse struct {
	Namespaces []storage.Namespace `json:"namespaces"`
	Count      int                 `json:"count"`
}

// HandleNamespaces lists namespaces (GET) and creates one (POST). Both are
// SUPER per ADR-027, and visibility is per-namespace — a USER seeing the list
// would be seeing across.
func (s *QNTXServer) HandleNamespaces(w http.ResponseWriter, r *http.Request) {
	namespaces, ok := s.superNamespaces(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		found, err := namespaces.List()
		if err != nil {
			writeRichError(w, s.logger, errors.Wrap(err, "failed to list namespaces"),
				http.StatusInternalServerError)
			return
		}
		if err := writeJSON(w, http.StatusOK, listNamespacesResponse{Namespaces: found, Count: len(found)}); err != nil {
			s.logger.Errorw("failed to write the namespace list", "error", err)
		}

	case http.MethodPost:
		s.createNamespace(w, r, namespaces)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleNamespaceByName is the switch on one namespace, and its ending.
//
//	POST   /api/namespaces/{name}/disable
//	POST   /api/namespaces/{name}/enable
//	DELETE /api/namespaces/{name}
func (s *QNTXServer) HandleNamespaceByName(w http.ResponseWriter, r *http.Request) {
	namespaces, ok := s.superNamespaces(w, r)
	if !ok {
		return
	}

	const prefix = "/api/namespaces/"
	name, verb, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if name == "" {
		http.Error(w, "no namespace in "+r.URL.Path, http.StatusBadRequest)
		return
	}

	// You cannot switch off or end the namespace you are standing in. The UI
	// says so by refusing the right-click; this is what makes it true for a
	// caller that never opened the UI.
	if admitted, gated := auth.AdmissionFrom(r.Context()); gated {
		if standing := s.namespaceOf(admitted); slug.Of(standing) == slug.Of(name) {
			http.Error(w, "you are standing in "+standing+"; step somewhere else first",
				http.StatusConflict)
			return
		}
	}

	if r.Method == http.MethodDelete && verb == "" {
		s.deleteNamespace(w, r, namespaces, name)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST switches a namespace, DELETE ends one", http.StatusMethodNotAllowed)
		return
	}
	switch verb {
	case "disable":
		s.switchNamespace(w, r, namespaces, name, false)
	case "enable":
		s.switchNamespace(w, r, namespaces, name, true)
	default:
		http.Error(w, "no such verb on a namespace: "+verb, http.StatusNotFound)
	}
}

// switchNamespace puts one in or out of service. The store refuses system and
// default, because a disabled system is a node that cannot read who anybody is.
func (s *QNTXServer) switchNamespace(w http.ResponseWriter, r *http.Request, namespaces storage.Namespaces, name string, enabled bool) {
	if err := namespaces.SetEnabled(name, enabled); err != nil {
		writeRichError(w, s.logger, err, http.StatusBadRequest)
		return
	}
	// The door held from before the switch would keep serving what was just
	// switched off. Dropped either way: re-enabling reopens it on the next call.
	s.held.Forget(name)
	s.logger.Infow("namespace switched", "namespace", name, "enabled", enabled, "by", askedBy(r))
	w.WriteHeader(http.StatusNoContent)
}

// deleteNamespace ends one, draining what it held into default. The store
// refuses system, default, and a namespace still enabled.
func (s *QNTXServer) deleteNamespace(w http.ResponseWriter, r *http.Request, namespaces storage.Namespaces, name string) {
	// The door closes first. Its last flush writes what is still buffered, so
	// the drain below carries those rows too rather than leaving a tick to
	// write them into a prefix that is no longer there.
	s.held.Forget(name)
	if err := namespaces.Delete(name); err != nil {
		writeRichError(w, s.logger, err, http.StatusBadRequest)
		return
	}
	s.logger.Infow("namespace deleted", "namespace", name, "by", askedBy(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *QNTXServer) createNamespace(w http.ResponseWriter, r *http.Request, namespaces storage.Namespaces) {
	var req createNamespaceRequest
	// A namespace name is one path segment, so this body is small however
	// privileged the request. SUPER is not a reason to read what arrives.
	if err := json.NewDecoder(io.LimitReader(r.Body, maxNamespaceBodyBytes)).Decode(&req); err != nil {
		writeRichError(w, s.logger, errors.Wrap(err, "failed to decode the namespace request"),
			http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// An identity owns a namespace, so a request nobody was admitted for has
	// nobody to own what it would create.
	asked := askedBy(r)
	if asked == "" {
		writeRichError(w, s.logger,
			errors.New("this request carries no identity, so there is nobody to own a namespace"),
			http.StatusInternalServerError)
		return
	}

	// Who asked is the owner. It does not come from the request, because a
	// request naming its own owner names somebody else's.
	definition := storage.NamespaceDefinition{
		Owner:     asked,
		Enabled:   true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := namespaces.Create(req.Name, definition); err != nil {
		writeRichError(w, s.logger, err, http.StatusBadRequest)
		return
	}

	s.logger.Infow("namespace created", "namespace", req.Name, "by", definition.Owner)
	created := storage.Namespace{Name: req.Name, Definition: &definition, Kinds: []string{}}
	if err := writeJSON(w, http.StatusCreated, created); err != nil {
		s.logger.Errorw("failed to write the created namespace", "error", err, "namespace", req.Name)
	}
}

// superNamespaces answers both questions a namespace route has: does this
// backend keep namespaces, and was this request admitted at SUPER.
func (s *QNTXServer) superNamespaces(w http.ResponseWriter, r *http.Request) (storage.Namespaces, bool) {
	known := s.held.Known()
	if known == nil {
		// Which store is running is the whole of the answer: nothing the caller
		// can send makes this route work. See ADR-026 — the reference stays in
		// the source, where somebody can go and read it.
		said := "namespaces exist only on the parquet backend, and this node keeps every attestation in one namespace"
		if s.store != "" {
			said = "namespaces exist only on the parquet backend; this node runs the " + s.store +
				" backend, which keeps every attestation in one namespace"
		}
		http.Error(w, said, http.StatusNotImplemented)
		return nil, false
	}

	// Which levels reach this route is server/reach's. No caller means the route
	// ran outside Middleware, which is a wiring mistake rather than a refusal.
	if _, ok := auth.AdmissionFrom(r.Context()); !ok {
		http.Error(w, "refused", http.StatusForbidden)
		return nil, false
	}
	return known, true
}

// askedBy is the identity a request was admitted as, or empty when the route
// ran outside Middleware.
func askedBy(r *http.Request) string {
	if admitted, ok := auth.AdmissionFrom(r.Context()); ok {
		return admitted.Identity
	}
	return ""
}
