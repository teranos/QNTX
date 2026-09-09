package namespaces

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
)

// Universe is one namespace and everything it holds.
//
// "Namespaces don't mix and mesh. They are their own universes." (ADR-026)
//
// A namespace holds its attestations, its watchers, its schedules, its canvas.
// Reaching any of them is reaching this: a caller is admitted, the admission
// says which universe they are in, and the universe hands over what it has.
//
// The fields here are the whole of what a namespace holds, in one list, in one
// place. A backend names every one of them or does not compile, so what a
// universe is stays a question with an answer.
//
// The host runs universes. What the host has one of — the HTTP server, the
// plugin registry, its own DID, its connected clients — belongs on the server.
// What a namespace is made of belongs here.
type Universe struct {
	// name is the namespace as it was created, not the slug it is reached by.
	name string

	// store is the attestations of this universe.
	store ats.AttestationStore

	// watchers are the declarations that fire in this universe.
	watchers storage.Watchers
}

// NewUniverse is how a backend says what one of its namespaces holds.
func NewUniverse(name string, store ats.AttestationStore, watchers storage.Watchers) *Universe {
	return &Universe{name: name, store: store, watchers: watchers}
}

// Name is the namespace this universe is, as it was created.
func (u *Universe) Name() string {
	if u == nil {
		return ""
	}
	return u.name
}

// Store is the attestations of this universe.
func (u *Universe) Store() ats.AttestationStore {
	if u == nil {
		return nil
	}
	return u.store
}

// Watchers are the watcher declarations of this universe.
func (u *Universe) Watchers() storage.Watchers {
	if u == nil {
		return nil
	}
	return u.watchers
}

// Serving is the node's universes when it keeps one: the default, holding the
// store given. A backend with a single universe says what it holds this way,
// and so does a test that only needs somewhere for attestations to land.
func Serving(store ats.AttestationStore) *Held {
	held := &Held{}
	held.SetDefault(NewUniverse(auth.NamespaceDefault, store, nil))
	return held
}
