package server

import (
	"context"
	"github.com/teranos/QNTX/plugin"
	"io"
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
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
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar(), held: &namespaces.Held{}}
	s.held.SetKnown(known)
	return s
}

// callerAt is a call from tim at one level, standing in one namespace when
// one is named.
func callerAt(level auth.Level, standing string) context.Context {
	admitted := auth.Admitted(level)
	admitted.Identity = "https://mastodon.example/@tim"
	if standing != "" {
		admitted.Namespaces = []string{standing}
	}
	return auth.WithAdmission(context.Background(), admitted)
}

// askNamespaces asks one namespaces sigil, as every surface does.
// askNamespaces asks one sigil as every surface does (sigil.Ask): what it
// takes is refused before what answers it is asked.
func askNamespaces(t *testing.T, s *QNTXServer, ctx context.Context, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	signum := s.namespacesSignum()
	answer, held := signum.Answers[name]
	if !held {
		t.Fatalf("namespaces has no sigil %s", name)
	}
	for _, declared := range signum.GetSigils() {
		if declared.GetName() != name {
			continue
		}
		if refusal := sigil.Refuses(declared, sent); refusal != nil {
			return nil, refusal
		}
	}
	return answer(ctx, sent)
}

// A SQLite node keeps one universe. Answering with an empty list would say
// there are no namespaces, which is a different claim from having no such idea.
func TestANodeWithoutNamespacesSaysSoRatherThanListingNone(t *testing.T) {
	s := namespaceServer(t, nil)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, ""), "list", nil)

	if refusal == nil {
		t.Fatal("a node without namespaces listed some")
	}
	// The caller cannot read an ADR and cannot act on a reference to one. What
	// they can act on is which backend is running, so that is what is said.
	said := refusal.GetSays()
	if strings.Contains(said, "ADR") {
		t.Errorf("the answer cites an internal document: %q", said)
	}
	if !strings.Contains(said, "parquet") {
		t.Errorf("the answer does not name the backend that has namespaces: %q", said)
	}
}

// Which levels reach this is server/reach's:
// TestTheTableSaysWhoReachesTheNamespaces.

// No caller means the sigil was asked outside the gate, which is a wiring
// mistake. Treating it as anonymous-and-allowed is how an open endpoint happens.
func TestNoCallerIsRefused(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, context.Background(), "list", nil)

	if refusal.GetWhy() != sigil.NotAllowed {
		t.Errorf("refusal = %v, want %s", refusal, sigil.NotAllowed)
	}
	if fake.listed {
		t.Error("a call from nobody reached the store")
	}
}

// The toggle is one sigil each way, and which way it went is the whole
// message. What was switched is given back.
func TestTheSwitchSaysWhichWayItWent(t *testing.T) {
	for _, verb := range []struct {
		sigil string
		want  bool
	}{{"disable", false}, {"enable", true}} {
		fake := &fakeNamespaces{}
		s := namespaceServer(t, fake)

		answer, refusal := askNamespaces(t, s, callerAt(auth.LevelSuper, ""), verb.sigil, sigil.Sent{"name": "pond"})

		if refusal != nil {
			t.Fatalf("%s: refused: %s", verb.sigil, refusal.GetSays())
		}
		if got := answer.(*protocol.NamespaceActedOn).GetName(); got != "pond" {
			t.Errorf("%s: gave back %q, want pond", verb.sigil, got)
		}
		if fake.switched != "pond" {
			t.Errorf("%s: switched %q, want pond", verb.sigil, fake.switched)
		}
		if fake.switchedTo != verb.want {
			t.Errorf("%s: switched to %v, want %v", verb.sigil, fake.switchedTo, verb.want)
		}
	}
}

// delete ends the namespace it names, and says which.
func TestDeleteEndsTheNamespaceItNames(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	answer, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, auth.NamespaceSystem), "delete", sigil.Sent{"name": "pond"})

	if refusal != nil {
		t.Fatalf("refused: %s", refusal.GetSays())
	}
	if fake.deleted != "pond" {
		t.Errorf("deleted %q, want pond", fake.deleted)
	}
	if got := answer.(*protocol.NamespaceActedOn).GetName(); got != "pond" {
		t.Errorf("gave back %q, want pond", got)
	}
}

// A store that refuses is the whole of the answer, and the refusal travels.
func TestARefusedSwitchIsNotReportedAsDone(t *testing.T) {
	fake := &fakeNamespaces{err: errors.New("system cannot be switched off")}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelSuper, ""), "disable", sigil.Sent{"name": "system"})

	if refusal == nil {
		t.Fatal("a refused switch answered as though it happened")
	}
	if !strings.Contains(refusal.GetSays(), "cannot be switched off") {
		t.Errorf("the refusal did not travel: %q", refusal.GetSays())
	}
}

// You cannot switch off or end the namespace you are standing in. The UI says
// so by refusing the right-click; this is the same rule for a caller that never
// opened it.
func TestTheNamespaceYouAreStandingInIsNotYoursToEnd(t *testing.T) {
	for _, name := range []string{"disable", "delete"} {
		fake := &fakeNamespaces{}
		s := namespaceServer(t, fake)

		_, refusal := askNamespaces(t, s, callerAt(auth.LevelSuper, "pond"), name, sigil.Sent{"name": "pond"})

		if refusal.GetWhy() != sigil.NotAllowed {
			t.Errorf("%s: refusal = %v, want %s", name, refusal, sigil.NotAllowed)
		}
		if fake.switched != "" || fake.deleted != "" {
			t.Errorf("%s: reached the store while standing in it: switched %q deleted %q",
				name, fake.switched, fake.deleted)
		}
	}
}

// You stand in the node to empty the project. Standing in the thing being
// emptied is the one place this could be pressed by accident.
func TestNukingIsReachedFromSystem(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	answer, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, auth.NamespaceSystem), "nuke", nil)

	if refusal != nil {
		t.Fatalf("refused: %s", refusal.GetSays())
	}
	if !fake.nuked {
		t.Error("standing in system did not reach the store")
	}
	if got := answer.(*protocol.NamespaceActedOn).GetName(); got != auth.NamespaceDefault {
		t.Errorf("gave back %q, want %s", got, auth.NamespaceDefault)
	}
}

// Anywhere else is refused, default most of all: emptying what you are standing
// in is the thing the rectangle exists to prevent.
func TestNukingFromAnywhereElseIsRefused(t *testing.T) {
	for _, standing := range []string{auth.NamespaceDefault, "pond"} {
		fake := &fakeNamespaces{}
		s := namespaceServer(t, fake)

		_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, standing), "nuke", nil)

		if refusal.GetWhy() != sigil.NotAllowed {
			t.Errorf("standing in %s: refusal = %v, want %s", standing, refusal, sigil.NotAllowed)
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
		s := namespaceServer(t, fake)

		_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, standing), "delete", sigil.Sent{"name": "pond"})

		if refusal.GetWhy() != sigil.NotAllowed {
			t.Errorf("standing in %s: refusal = %v, want %s", standing, refusal, sigil.NotAllowed)
		}
		if fake.deleted != "" {
			t.Errorf("standing in %s reached the store and deleted %q", standing, fake.deleted)
		}
	}
}

// The table admits SUPER for the switch, so the level is the answer's to refuse.
func TestEndingANamespaceIsRootsAlone(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelSuper, auth.NamespaceSystem), "delete", sigil.Sent{"name": "pond"})

	if refusal.GetWhy() != sigil.NotAllowed {
		t.Errorf("refusal = %v, want %s", refusal, sigil.NotAllowed)
	}
	if fake.deleted != "" {
		t.Errorf("SUPER reached the store and deleted %q", fake.deleted)
	}
}

// A call naming no namespace would otherwise reach the store with an empty name.
func TestNamingNoNamespaceIsRefused(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, auth.NamespaceSystem), "delete", sigil.Sent{"name": ""})

	if refusal.GetWhy() != sigil.Missing {
		t.Errorf("refusal = %v, want %s", refusal, sigil.Missing)
	}
	if fake.deleted != "" {
		t.Errorf("an empty name reached the store as %q", fake.deleted)
	}
}

func TestRootListsNamespaces(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	answer, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, ""), "list", nil)

	if refusal != nil {
		t.Fatalf("refused: %s", refusal.GetSays())
	}
	if !fake.listed {
		t.Error("the store was never asked")
	}
	if listed := answer.(*protocol.NamespacesList); listed.GetCount() != 1 {
		t.Errorf("count = %d, want 1", listed.GetCount())
	}
}

// A store that refuses says why — a name that is taken, a name that is a path.
// Swallowing that would make creation look like it worked.
func TestARefusedCreationSaysWhy(t *testing.T) {
	fake := &fakeNamespaces{err: errors.New("namespace pond already exists and already has an owner")}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, ""), "create", sigil.Sent{"name": "pond"})

	if refusal.GetWhy() != sigil.Invalid {
		t.Fatalf("refusal = %v, want %s", refusal, sigil.Invalid)
	}
	if refusal.GetSays() == "" {
		t.Error("the refusal carried no message")
	}
}

// An identity inside QNTX owns a namespace, and who asked is that identity. A
// namespace is created enabled.
func TestWhoAskedIsWhoOwnsIt(t *testing.T) {
	fake := &fakeNamespaces{}
	s := namespaceServer(t, fake)

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, ""), "create", sigil.Sent{"name": "pond"})

	if refusal != nil {
		t.Fatalf("refused: %s", refusal.GetSays())
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

	_, refusal := askNamespaces(t, s, callerAt(auth.LevelRoot, ""), "create", sigil.Sent{"name": ""})

	if refusal.GetWhy() != sigil.Missing {
		t.Errorf("refusal = %v, want %s", refusal, sigil.Missing)
	}
	if fake.created != "" {
		t.Errorf("the store was asked to create %q", fake.created)
	}
}
