// Package namespaces holds the node's universes and is the only way to reach
// one.
//
// A namespace is reached by being handed a store, never by naming one. The
// method that opens a store by name is unexported and stays in this package,
// so a handler cannot call it: it asks at one of the doors instead, and each
// door is a different promise. Read hands back something that cannot write.
// Write refuses a namespace the admission may not reach. WriteAsPublic is the
// one door for input that no admission stands behind, and it refuses the two
// namespaces that hold the node's own records.
//
// The same move server/reach made for routes: what would let a caller do the
// wrong thing does not leave the package (ADR-026, ADR-027).
package namespaces

import (
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// Opener opens one namespace: everything it holds, named at once. A backend
// that keeps namespaces has one; the rest keep a single universe and set none.
type Opener interface {
	OpenNamespace(name string) (*Universe, error)
}

// Reading is the read half of an attestation store: what the node's own
// lookups want and the whole of what they may have. A caller holding one
// cannot write, so a lookup in system cannot become a write there by mistake.
type Reading interface {
	GetAttestations(filters ats.AttestationFilter) ([]*types.As, error)
}

// Held is the node's namespaces: the two it always has, and the ones it opens
// as they are asked for.
//
// A node serves the universes it has been handed. One that has been handed none
// says so when asked for one, which is what a server given nothing should do.
//
// open is keyed by slug and not by what was asked for: "Clean" and "clean" are
// one namespace, so they are one open store and never two.
type Held struct {
	mu     sync.Mutex
	open   map[string]*Universe
	dflt   *Universe
	system *Universe
	known  storage.Namespaces
	opener Opener
	// starting is what a namespace does when it starts. Held opens a namespace;
	// what a namespace then runs for itself is the server's, so it is handed in
	// rather than known here.
	starting func(*Universe)
	logger   *zap.SugaredLogger
}

// SetDefault names the universe a caller who names none acts in.
func (h *Held) SetDefault(u *Universe) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dflt = u
}

// SetSystem names where the node keeps what it knows about itself. Nil is a
// backend that keeps no separate one.
func (h *Held) SetSystem(u *Universe) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.system = u
}

// SetKnown gives the backend's namespace list, which is what says whether a
// name is a namespace at all. Nil is a backend that keeps one universe.
func (h *Held) SetKnown(known storage.Namespaces) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.known = known
}

// SetOpener gives a way to reach a namespace created after the node started.
// Without it only the two it always has are reachable.
func (h *Held) SetOpener(opener Opener) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opener = opener
}

// SetStarting names what a namespace runs when it starts. It is called once
// per namespace, as the namespace is opened, and never while the lock is held:
// what a namespace starts may reach back for the namespace that is starting.
func (h *Held) SetStarting(starting func(*Universe)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.starting = starting
}

// SetLogger names where opening a namespace is said out loud.
func (h *Held) SetLogger(logger *zap.SugaredLogger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logger = logger
}

// Known is the backend's namespace list, or nil on a backend that keeps one
// universe. The routes answer that rather than pretending there is a list.
func (h *Held) Known() storage.Namespaces {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.known
}

// KeepsSystem reports whether this node has a separate system store. Without
// one there are no lines in it to read, and that is not an error to say.
func (h *Held) KeepsSystem() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.system != nil
}

// Served is the universe a caller who names no namespace acts in, handed out
// whole. The subsystems that are not about namespaces at all — plugins,
// embeddings, the type registry, the watcher engine — hold this one and never
// ask for another.
func (h *Held) Served() ats.AttestationStore {
	return h.ServedUniverse().Store()
}

// ServedUniverse is that same universe whole, for the callers that need more of
// it than its attestations.
func (h *Held) ServedUniverse() *Universe {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dflt
}

// TheNodesOwnRecords is where the node writes about itself, and it is the one
// writer here that no admission stands behind: the /auth/… routes are ANYONE
// because logging in cannot ask you to be logged in.
//
// What bounds it is not this package. The predicates are a closed vocabulary
// the node writes about itself and the actor is the node's own DID, both in
// server/auth. A backend that keeps no separate system store falls back to the
// one it has: the record is worth more in the wrong namespace than not written
// at all (ADR-027).
func (h *Held) TheNodesOwnRecords() ats.AttestationStore {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.system != nil {
		return h.system.Store()
	}
	return h.dflt.Store()
}

// Read hands back a store that cannot write.
//
// The node reads its own lines — roles, words, reach, stand definitions — out
// of system in several places. Each of them holds this and so none of them can
// write there, which is most of what the audit in ADR-027 had to check by
// hand.
func (h *Held) Read(namespace string) (Reading, error) {
	u, err := h.universeIn(namespace)
	if err != nil {
		return nil, err
	}
	return u.Store(), nil
}

// Write hands back a store for the namespace an admission acts in. system is
// visible to SUPER and above and to a role held in system, and nothing else
// decides (ADR-027).
//
// The admission is a parameter rather than something read off a request
// elsewhere, so a write cannot be made without passing the thing that would
// refuse it.
func (h *Held) Write(admitted auth.Admission, namespace string) (ats.AttestationStore, error) {
	u, err := h.Universe(admitted, namespace)
	if err != nil {
		return nil, err
	}
	return u.Store(), nil
}

// Universe hands back the universe an admission acts in, whole. Same guard as
// Write, and the door for everything a namespace holds beyond its attestations.
func (h *Held) Universe(admitted auth.Admission, namespace string) (*Universe, error) {
	if namespace == auth.NamespaceSystem && !admitted.MaySeeSystem() {
		return nil, NotServed{Asked: namespace}
	}
	return h.universeIn(namespace)
}

// WriteWhatTheNodeKnowsOfItself hands back the system store for a line about
// the node rather than about the world: a role grant, a reach line, a word
// line, a stand's definition. These land in system whatever namespace their
// writer acts in, because that is where the node keeps what it knows about
// itself.
//
// Writing there is not seeing there. Who may write one of these is settled
// before this is called — by the reach table for the route, and by the grant
// and reach checks in the handler — and a coordinator who may grant a role in
// their own namespace does not thereby see system. So this door asks for no
// admission, and takes its name from what it is for (ADR-027).
func (h *Held) WriteWhatTheNodeKnowsOfItself() (ats.AttestationStore, error) {
	u, err := h.universeIn(auth.NamespaceSystem)
	if err != nil {
		return nil, err
	}
	return u.Store(), nil
}

// WriteAsPublic hands back a store for input that no admission stands behind —
// a stand's arrivals off an ANYONE route (ADR-035).
//
// system and default hold the node's own records: users, tokens, grants. A
// stranger's write does not land in either, and the refusal is here rather
// than in the handler so that a second public writer cannot be added without
// it.
func (h *Held) WriteAsPublic(namespace string) (ats.AttestationStore, error) {
	if !PublicMay(namespace) {
		return nil, NotServed{Asked: namespace}
	}
	u, err := h.universeIn(namespace)
	if err != nil {
		return nil, err
	}
	return u.Store(), nil
}

// PublicMay reports whether a namespace may take a write that no admission
// stands behind: any namespace but the two the node keeps for itself.
func PublicMay(namespace string) bool {
	return namespace != "" &&
		namespace != auth.NamespaceSystem &&
		namespace != auth.NamespaceDefault
}

// universeIn returns one namespace whole, opening it the first time it is asked
// for. Unexported, and every door above goes through it: a caller that could
// reach this could name a namespace nothing decided it may have.
func (h *Held) universeIn(namespace string) (*Universe, error) {
	// A caller reaches a universe by being given one, and this node was given
	// none to give. Saying so is the answer; crashing on the way is not.
	if h == nil {
		return nil, NotServed{Asked: namespace}
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	switch namespace {
	case auth.NamespaceDefault:
		return h.dflt, nil
	case auth.NamespaceSystem:
		if h.system == nil {
			return nil, NotServed{Asked: namespace}
		}
		return h.system, nil
	}
	if h.opener == nil || h.known == nil {
		return nil, NotServed{Asked: namespace}
	}
	if u, ok := h.open[slug.Of(namespace)]; ok {
		return u, nil
	}

	// Opening writes a prefix at the location on the first flush, so a name
	// nobody created would become the namespace it misspelled.
	known, err := h.known.List()
	if err != nil {
		return nil, errors.Wrapf(err, "cannot tell whether %s exists", namespace)
	}
	// A door is keyed by slug and a namespace keeps the name it was created
	// with, so what opens the store is the store's name and never the key.
	name, err := Named(known, namespace)
	if err != nil {
		return nil, err
	}

	u, err := h.opener.OpenNamespace(name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the universe %s", name)
	}
	if h.open == nil {
		h.open = map[string]*Universe{}
	}
	h.open[slug.Of(name)] = u
	if h.logger != nil {
		h.logger.Infow("Opened a namespace", "namespace", name, "reached_by", slug.Of(name))
	}

	// A namespace that has been opened has not yet run. Starting it is done
	// with the lock released, and once: the map holds it before this returns,
	// so a second caller reaching the same namespace is handed what is already
	// running rather than starting it again.
	if h.starting != nil {
		starting := h.starting
		h.mu.Unlock()
		starting(u)
		h.mu.Lock()
	}
	return u, nil
}
