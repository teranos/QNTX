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

func admittedAt(r *http.Request, level auth.Level) *http.Request {
	return r.WithContext(auth.WithAdmission(r.Context(), auth.Admission{
		Level:    level,
		Identity: "https://mastodon.example/@tim",
	}))
}

// A SQLite node keeps one universe. Answering with an empty list would say
// there are no namespaces, which is a different claim from having no such idea.
func TestANodeWithoutNamespacesSaysSoRatherThanListingNone(t *testing.T) {
	s := namespaceServer(t, nil)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, admittedAt(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelRoot))

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}

	// The caller cannot read an ADR and cannot act on a reference to one. What
	// they can act on is which backend is running, so that is what is said.
	said := w.Body.String()
	if strings.Contains(said, "ADR") {
		t.Errorf("the answer cites an internal document: %q", said)
	}
	if !strings.Contains(said, "parquet") {
		t.Errorf("the answer does not name the backend that has namespaces: %q", said)
	}
}

// Visibility is per-namespace (ADR-027), so anyone below reading the list would
// be reading across. SUPER is the boss of its own namespace, and the list of
// them all is not inside any one of them.
func TestOnlyRootReachesTheListOfNamespaces(t *testing.T) {
	for _, level := range []auth.Level{auth.LevelSuper, auth.LevelToken, auth.LevelAttestor} {
		fake := &fakeNamespaces{}
		s := namespaceServer(t, fake)
		w := httptest.NewRecorder()

		s.HandleNamespaces(w, admittedAt(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), level))

		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d", level, w.Code, http.StatusForbidden)
		}
		if fake.listed {
			t.Errorf("%s: the store was asked by a caller that is not ROOT", level)
		}
	}
}

// A token reaches what its minter reaches and is not the minter. Creating a
// namespace is how a token would leave the scope it was minted inside of.
func TestATokenMayNotCreateANamespace(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/namespaces", jsonBody(`{"name":"pond"}`))
	s.HandleNamespaces(w, admittedAt(req, auth.LevelToken))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if fake.created != "" {
		t.Errorf("a token created the namespace %q", fake.created)
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

func TestRootListsNamespaces(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	s.HandleNamespaces(w, admittedAt(httptest.NewRequest(http.MethodGet, "/api/namespaces", nil), auth.LevelRoot))

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
	s.HandleNamespaces(w, admittedAt(req, auth.LevelRoot))

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
	s.HandleNamespaces(w, admittedAt(req, auth.LevelRoot))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.created != "" {
		t.Errorf("the store was asked to create %q", fake.created)
	}
}
