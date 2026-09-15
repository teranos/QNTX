package server

import (
	"github.com/teranos/QNTX/server/namespaces"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// fakeNamespaces records what it was asked so a refusal can be told from a
// call that went through and happened to fail.
type fakeNamespaces struct {
	listed  bool
	created string
	defined storage.NamespaceDefinition
	// switched is the name SetEnabled was asked about and what it was asked to
	// make it. An empty name is nobody having asked.
	switched   string
	switchedTo bool
	deleted    string
	nuked      bool
	err        error
}

func (f *fakeNamespaces) List() ([]storage.Namespace, error) {
	f.listed = true
	return []storage.Namespace{{Name: "default"}}, f.err
}

func (f *fakeNamespaces) Create(name string, definition storage.NamespaceDefinition) error {
	f.created = name
	f.defined = definition
	return f.err
}

func (f *fakeNamespaces) SetEnabled(name string, enabled bool) error {
	f.switched = name
	f.switchedTo = enabled
	return f.err
}

func (f *fakeNamespaces) Delete(name string) error {
	f.deleted = name
	return f.err
}

func (f *fakeNamespaces) Nuke() error {
	f.nuked = true
	return f.err
}

func jsonBody(body string) io.Reader {
	return strings.NewReader(body)
}

func namespaceServer(t *testing.T, known storage.Namespaces) *QNTXServer {
	t.Helper()
	s := &QNTXServer{logger: zap.NewNop().Sugar(), held: &namespaces.Held{}}
	s.held.SetKnown(known)
	return s
}

func admittedAt(r *http.Request, level auth.Level) *http.Request {
	admitted := auth.Admitted(level)
	admitted.Identity = "https://mastodon.example/@tim"
	return r.WithContext(auth.WithAdmission(r.Context(), admitted))
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

// Which levels reach this route is server/reach's:
// TestTheTableSaysWhoReachesTheNamespaces.

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

// byName runs one request against the switch on a single namespace.
func byName(t *testing.T, fake *fakeNamespaces, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()
	s.HandleNamespaceByName(w, admittedAt(httptest.NewRequest(method, path, nil), auth.LevelSuper))
	return w
}

// The toggle is one verb each way, and which way it went is the whole message.
func TestTheSwitchSaysWhichWayItWent(t *testing.T) {
	for _, verb := range []struct {
		path string
		want bool
	}{{"disable", false}, {"enable", true}} {
		fake := &fakeNamespaces{}
		w := byName(t, fake, http.MethodPost, "/api/namespaces/pond/"+verb.path)

		if w.Code != http.StatusNoContent {
			t.Errorf("%s: status = %d, want %d", verb.path, w.Code, http.StatusNoContent)
		}
		if fake.switched != "pond" {
			t.Errorf("%s: switched %q, want pond", verb.path, fake.switched)
		}
		if fake.switchedTo != verb.want {
			t.Errorf("%s: switched to %v, want %v", verb.path, fake.switchedTo, verb.want)
		}
	}
}

// A verb nothing answers to must not fall through to one that does.
func TestAnUnknownVerbTouchesNothing(t *testing.T) {
	fake := &fakeNamespaces{}
	w := byName(t, fake, http.MethodPost, "/api/namespaces/pond/nuke")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if fake.switched != "" || fake.deleted != "" {
		t.Errorf("an unknown verb reached the store: switched %q deleted %q", fake.switched, fake.deleted)
	}
}

// DELETE on the namespace itself ends it. The verb paths are for the switch,
// which HTTP has no method for.
func TestDeleteEndsTheNamespaceNamedInThePath(t *testing.T) {
	fake := &fakeNamespaces{}
	w := standingIn(t, fake, auth.NamespaceSystem, http.MethodDelete, "/api/namespaces/pond",
		(*QNTXServer).HandleNamespaceByName)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if fake.deleted != "pond" {
		t.Errorf("deleted %q, want pond", fake.deleted)
	}
}

// A store that refuses is the whole of the answer, and the refusal travels.
func TestARefusedSwitchIsNotReportedAsDone(t *testing.T) {
	fake := &fakeNamespaces{err: errors.New("system cannot be switched off")}
	w := byName(t, fake, http.MethodPost, "/api/namespaces/system/disable")

	if w.Code == http.StatusNoContent {
		t.Fatal("a refused switch answered as though it happened")
	}
	if !strings.Contains(w.Body.String(), "cannot be switched off") {
		t.Errorf("the refusal did not travel: %q", w.Body.String())
	}
}

// You cannot switch off or end the namespace you are standing in. The UI says
// so by refusing the right-click; this is the same rule for a caller that never
// opened it.
func TestTheNamespaceYouAreStandingInIsNotYoursToEnd(t *testing.T) {
	for _, path := range []string{"/api/namespaces/pond/disable", "/api/namespaces/pond"} {
		fake := &fakeNamespaces{}
		s := namespaceServer(t, fake)
		w := httptest.NewRecorder()

		method := http.MethodPost
		if path == "/api/namespaces/pond" {
			method = http.MethodDelete
		}
		standing := auth.Admitted(auth.LevelSuper)
		standing.Identity = "https://mastodon.example/@tim"
		standing.Namespaces = []string{"pond"}
		r := httptest.NewRequest(method, path, nil)
		s.HandleNamespaceByName(w, r.WithContext(auth.WithAdmission(r.Context(), standing)))

		if w.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want %d", path, w.Code, http.StatusConflict)
		}
		if fake.switched != "" || fake.deleted != "" {
			t.Errorf("%s: reached the store while standing in it: switched %q deleted %q",
				path, fake.switched, fake.deleted)
		}
	}
}

// standingIn runs a request from a caller who is in one namespace.
func standingIn(t *testing.T, fake *fakeNamespaces, namespace, method, path string,
	run func(*QNTXServer, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://mastodon.example/@tim"
	if namespace != "" {
		admitted.Namespaces = []string{namespace}
	}
	r := httptest.NewRequest(method, path, nil)
	run(s, w, r.WithContext(auth.WithAdmission(r.Context(), admitted)))
	return w
}

// You stand in the node to empty the project. Standing in the thing being
// emptied is the one place this could be pressed by accident.
func TestNukingIsReachedFromSystem(t *testing.T) {
	fake := &fakeNamespaces{}
	w := standingIn(t, fake, auth.NamespaceSystem, http.MethodPost, "/api/namespaces/default/nuke",
		(*QNTXServer).HandleNukeDefault)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if !fake.nuked {
		t.Error("standing in system did not reach the store")
	}
}

// Anywhere else is refused, default most of all: emptying what you are standing
// in is the thing the rectangle exists to prevent.
func TestNukingFromAnywhereElseIsRefused(t *testing.T) {
	for _, standing := range []string{auth.NamespaceDefault, "pond"} {
		fake := &fakeNamespaces{}
		w := standingIn(t, fake, standing, http.MethodPost, "/api/namespaces/default/nuke",
			(*QNTXServer).HandleNukeDefault)

		if w.Code != http.StatusConflict {
			t.Errorf("standing in %s: status = %d, want %d", standing, w.Code, http.StatusConflict)
		}
		if fake.nuked {
			t.Errorf("standing in %s reached the store", standing)
		}
	}
}

// Which level reaches it is server/reach's:
// TestTheTableKeepsNukingToRoot.

// "that you need to stand in system for it and you also need to be root for it"
func TestEndingANamespaceIsReachedFromSystem(t *testing.T) {
	for _, standing := range []string{auth.NamespaceDefault, "lake"} {
		fake := &fakeNamespaces{}
		w := standingIn(t, fake, standing, http.MethodDelete, "/api/namespaces/pond",
			(*QNTXServer).HandleNamespaceByName)

		if w.Code != http.StatusConflict {
			t.Errorf("standing in %s: status = %d, want %d", standing, w.Code, http.StatusConflict)
		}
		if fake.deleted != "" {
			t.Errorf("standing in %s reached the store and deleted %q", standing, fake.deleted)
		}
	}
}

// The route admits SUPER for the switch, so the level is the handler's to refuse.
func TestEndingANamespaceIsRootsAlone(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()
	super := auth.Admitted(auth.LevelSuper)
	super.Identity = "https://mastodon.example/@tim"
	super.Namespaces = []string{auth.NamespaceSystem}
	r := httptest.NewRequest(http.MethodDelete, "/api/namespaces/pond", nil)
	s.HandleNamespaceByName(w, r.WithContext(auth.WithAdmission(r.Context(), super)))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if fake.deleted != "" {
		t.Errorf("SUPER reached the store and deleted %q", fake.deleted)
	}
}

// A path naming no namespace would otherwise reach the store with an empty name.
func TestAPathNamingNoNamespaceIsRefused(t *testing.T) {
	fake := &fakeNamespaces{}
	w := byName(t, fake, http.MethodDelete, "/api/namespaces/")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.deleted != "" {
		t.Errorf("an empty name reached the store as %q", fake.deleted)
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

// An identity inside QNTX owns a namespace, and who asked is that identity. A
// namespace is created enabled.
func TestWhoAskedIsWhoOwnsIt(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)
	w := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/api/namespaces", jsonBody(`{"name":"pond"}`))
	s.HandleNamespaces(w, admittedAt(req, auth.LevelRoot))

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if fake.defined.Owner != "https://mastodon.example/@tim" {
		t.Errorf("owner = %q, want the identity that was admitted", fake.defined.Owner)
	}
	if !fake.defined.Enabled {
		t.Error("the namespace was created disabled")
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
