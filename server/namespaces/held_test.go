package namespaces

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/auth"
)

// heldNamespaces is a backend's namespace list, and nothing else.
type heldNamespaces struct{ names []string }

func (h heldNamespaces) List() ([]storage.Namespace, error) {
	var out []storage.Namespace
	for _, name := range h.names {
		out = append(out, storage.Namespace{Name: name})
	}
	return out, nil
}

func (heldNamespaces) Create(string, storage.NamespaceDefinition) error { return nil }

// An opener that only records what it was asked to open. Which store comes
// back is not what these tests are about; the name it is opened under is.
type openedNamespaces struct{ asked []string }

func (o *openedNamespaces) OpenNamespace(name string) (*Universe, error) {
	o.asked = append(o.asked, name)
	return nil, nil
}

func serving(names []string, opener Opener) *Held {
	held := &Held{}
	held.SetKnown(heldNamespaces{names: names})
	held.SetOpener(opener)
	return held
}

// The door says clean, the store says Clean. The door reaches it by slug, and
// the store is opened under the name the store keeps.
func TestADoorReachesItsNamespaceBySlug(t *testing.T) {
	opener := &openedNamespaces{}
	held := serving([]string{"Clean"}, opener)

	_, err := held.Read("clean")
	require.NoError(t, err, "a door keyed clean did not reach the namespace Clean")
	assert.Equal(t, []string{"Clean"}, opener.asked,
		"the store was opened under the door's key rather than its own name")
}

// A second ask is the same namespace and not a second store: the slug is what
// is remembered, so Clean and clean are one open store and not two.
func TestANamespaceOpensOnce(t *testing.T) {
	opener := &openedNamespaces{}
	held := serving([]string{"Clean"}, opener)

	for _, asked := range []string{"clean", "Clean"} {
		if _, err := held.Read(asked); err != nil {
			t.Fatalf("%q was refused: %v", asked, err)
		}
	}
	assert.Len(t, opener.asked, 1, "one namespace was opened twice")
}

// Two namespaces with one slug is a question the node cannot answer, and
// answering it by list order would put somebody in a universe nobody chose.
func TestTwoNamespacesWithOneSlugIsRefused(t *testing.T) {
	opener := &openedNamespaces{}
	held := serving([]string{"Clean", "clean"}, opener)

	_, err := held.Read("clean")
	require.Error(t, err, "a door reached one of two namespaces that share a slug")
	assert.Contains(t, err.Error(), "Clean")
	assert.Contains(t, err.Error(), "clean")
	assert.Empty(t, opener.asked, "an ambiguous namespace was opened anyway")
}

// A namespace nothing on the store answers to is still not served.
func TestANamespaceTheStoreDoesNotHaveIsRefused(t *testing.T) {
	held := serving([]string{"Clean"}, &openedNamespaces{})

	_, err := held.Read("pond")
	require.Error(t, err, "a door reached a namespace this node does not have")
	assert.Contains(t, err.Error(), "pond")
}

// system is visible to SUPER and above, and to a role held in system. The
// door takes the admission, so a write cannot be asked for without passing the
// thing that refuses it (ADR-027).
func TestWritingSystemNeedsMaySeeSystem(t *testing.T) {
	held := &Held{}
	held.SetSystem(mustMake("system", nothing{}, nil))

	_, err := held.Write(auth.Admitted(auth.LevelAttestor), auth.NamespaceSystem)
	require.Error(t, err, "an attestor wrote where the node keeps its own records")
	assert.Contains(t, err.Error(), auth.NamespaceSystem)

	_, err = held.Write(auth.Admitted(auth.LevelSuper), auth.NamespaceSystem)
	require.NoError(t, err, "SUPER was refused the system namespace")
}

// A write no admission stands behind never lands in the two namespaces that
// hold the node's own records, whatever the handler asks for (ADR-035).
func TestPublicWritesNeverReachSystemOrDefault(t *testing.T) {
	held := &Held{}
	held.SetDefault(mustMake("default", nothing{}, nil))
	held.SetSystem(mustMake("system", nothing{}, nil))

	for _, asked := range []string{auth.NamespaceSystem, auth.NamespaceDefault, ""} {
		_, err := held.WriteAsPublic(asked)
		require.Errorf(t, err, "a public write reached %q", asked)
	}
}

// nothing is a store that is never called: these tests are about which door
// hands one back, not about what it does.
type nothing struct{ ats.AttestationStore }

// watchersOf is a watcher store that only says which universe it belongs to.
type watchersOf struct {
	storage.Watchers
	namespace string
}

// eachHoldsItsOwn is an opener that gives every namespace its own watchers.
type eachHoldsItsOwn struct{}

func (eachHoldsItsOwn) OpenNamespace(name string) (*Universe, error) {
	return mustMake(name, nothing{}, watchersOf{namespace: name}), nil
}

// A namespace holds its own watchers. Reaching one reaches what it holds, and
// what the default holds is not what it holds.
func TestAUniverseHoldsItsOwnWatchers(t *testing.T) {
	held := serving([]string{"clean", "harbour"}, eachHoldsItsOwn{})
	held.SetDefault(mustMake("default", nothing{}, watchersOf{namespace: "default"}))

	for _, name := range []string{"clean", "harbour"} {
		universe, err := held.Universe(auth.Admission{}, name)
		if err != nil {
			t.Fatalf("%s was not served: %v", name, err)
		}
		if universe.Name() != name {
			t.Fatalf("asked for %s and was handed %s", name, universe.Name())
		}
		got, ok := universe.Watchers().(watchersOf)
		if !ok {
			t.Fatalf("%s holds no watchers of its own", name)
		}
		if got.namespace != name {
			t.Fatalf("%s was handed the watchers of %s", name, got.namespace)
		}
	}

	if got := held.ServedUniverse().Watchers().(watchersOf); got.namespace != "default" {
		t.Fatalf("the default was handed the watchers of %s", got.namespace)
	}
}

// A namespace opened after the node booted has not run yet. Starting it is what
// makes it the same universe as one the node booted with, so it happens as the
// namespace is opened, and once however many callers reach it.
func TestANamespaceStartsWhenItIsOpened(t *testing.T) {
	held := serving([]string{"clean", "harbour"}, eachHoldsItsOwn{})

	var started []string
	held.SetStarting(func(u *Universe) {
		started = append(started, u.Name())
		// What a namespace starts may reach back for the namespace starting it,
		// which deadlocks if the lock is still held.
		if _, err := held.Universe(auth.Admission{}, u.Name()); err != nil {
			t.Errorf("a starting namespace could not reach itself: %v", err)
		}
	})

	for range 3 {
		if _, err := held.Universe(auth.Admission{}, "clean"); err != nil {
			t.Fatalf("clean was not served: %v", err)
		}
	}
	if _, err := held.Universe(auth.Admission{}, "harbour"); err != nil {
		t.Fatalf("harbour was not served: %v", err)
	}

	assert.Equal(t, []string{"clean", "harbour"}, started,
		"a namespace started other than once as it was opened")
}

// mustMake is a namespace made of what a test cares about and a stand-in for
// the rest, so a test about doors is not also a test about schedules.
func mustMake(name string, store ats.AttestationStore, watchers storage.Watchers) *Universe {
	if watchers == nil {
		watchers = stubWatchers{}
	}
	u, err := NewUniverse(name, Made{Store: store, Watchers: watchers, Schedules: &schedule.Store{}, Canvas: &glyphstorage.CanvasStore{}, Embeddings: &storage.EmbeddingStore{}, Rich: &storage.BoundedStore{}, Executions: &schedule.ExecutionStore{}, Prompts: &prompt.PromptStore{}, Aliases: &storage.AliasStore{}, Queries: &storage.SQLQueryStore{}})
	if err != nil {
		panic(err)
	}
	return u
}

type stubWatchers struct{ storage.Watchers }
