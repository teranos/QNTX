package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// fakeNamespaces records what it was asked so a refusal can be told from a
// call that went through and happened to fail.
type fakeNamespaces struct {
	listed  bool
	created string
	err     error
}

func (f *fakeNamespaces) List() ([]storage.Namespace, error) {
	f.listed = true
	return []storage.Namespace{{Name: "default"}}, f.err
}

func (f *fakeNamespaces) Create(name string, _ storage.NamespaceOwner) error {
	f.created = name
	return f.err
}

func jsonBody(body string) io.Reader {
	return strings.NewReader(body)
}

func namespaceServer(t *testing.T, namespaces storage.Namespaces) *QNTXServer {
	t.Helper()
	return &QNTXServer{
		namespaces: namespaces,
		logger:     zap.NewNop().Sugar(),
		nodeDID:    &nodedid.Handler{DID: "did:key:znode"},
	}
}

func asCaller(r *http.Request, level auth.Level) *http.Request {
	return r.WithContext(auth.WithCaller(r.Context(), auth.Caller{
		Level:    level,
		Identity: "https://chaos.social/@groundskeeper",
	}))
}

// A SQLite node keeps one universe. Answering with an empty list would say
// there are no namespaces, which is a different claim from having no such idea.
func TestANodeWithoutNamespacesSaysSoRatherThanListingNone(t *testing.T) {
	s := namespaceServer(t, nil)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, asCaller(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelSuper))

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
}

// ADR-027 puts namespace management at SUPER, and visibility is per-namespace —
// a USER reading the list would be reading across.
func TestAUserMayNotEvenListNamespaces(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, asCaller(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelUser))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if fake.listed {
		t.Error("the store was asked despite the caller not being SUPER")
	}
}

// No caller means the route ran outside Middleware, which is a wiring mistake.
// Treating it as anonymous-and-allowed is how an open endpoint happens.
func TestNoCallerIsRefused(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, httptest.NewRequest(http.MethodGet, "/api/namespaces", nil))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestSuperListsNamespaces(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, asCaller(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelSuper))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !fake.listed {
		t.Error("the store was never asked")
	}
}

// A store that refuses says why — a name that is taken, a name that is a path.
// Swallowing that would make creation look like it worked.
func TestARefusedCreationSaysWhy(t *testing.T) {
	fake := &fakeNamespaces{err: errors.New("namespace pond already exists and already has an owner")}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/namespaces", jsonBody(`{"name":"pond"}`))
	s.HandleNamespaces(w, asCaller(req, auth.LevelSuper))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if body := w.Body.String(); body == "" {
		t.Error("the refusal carried no message")
	}
}

// A namespace with no name is not a namespace, and the store would refuse it
// anyway — this says so before writing anything.
func TestCreatingWithoutANameIsRefused(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/namespaces", jsonBody(`{"name":""}`))
	s.HandleNamespaces(w, asCaller(req, auth.LevelSuper))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.created != "" {
		t.Errorf("the store was asked to create %q", fake.created)
	}
}
