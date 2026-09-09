package namespaces

import (
	"reflect"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// Made is what a namespace is made of.
//
// "A namespace is its own universe inside of QNTX. It runs, in the same binary.
// It has a name, an owner, and its definition in its ns.toml. It is reached
// through a door configured in am.toml. It has its attestations, its watchers,
// its schedules, its canvas, its types — it is made of those, and it starts by
// running its own steps the way QNTX starts."
//
// A field here is a thing a namespace is made of, and every backend answers for
// every field: NewUniverse names the one left out, at the moment the backend
// says the namespace exists. Adding a thing a namespace is made of is adding a
// field here, and there is one place it can go.
type Made struct {
	// Store is the attestations.
	Store ats.AttestationStore
	// Watchers are the declarations that fire here.
	Watchers storage.Watchers
	// Schedules are the jobs that tick here.
	Schedules *schedule.Store
}

// Universe is one namespace: its name, and what it is made of.
//
// A caller is handed one. The host runs universes, and what the host has one of
// however many it runs — the HTTP server, the plugin registry, its own DID, its
// connected clients — is the host's.
type Universe struct {
	// name is the namespace as it was created, not the slug it is reached by.
	name string
	made Made
}

// NewUniverse is how a backend says what one of its namespaces is made of.
func NewUniverse(name string, made Made) (*Universe, error) {
	if name == "" {
		return nil, errors.New("a namespace is a name, and this one has none")
	}
	if unanswered := unanswered(made); len(unanswered) > 0 {
		return nil, errors.Newf("the namespace %s says nothing of what it is made of: %v", name, unanswered)
	}
	return &Universe{name: name, made: made}, nil
}

// unanswered is every field of Made this backend left at nothing.
//
// Reflection rather than a list, so a field added to Made is answered for by
// every backend without anybody adding it to a check as well.
func unanswered(made Made) []string {
	var quiet []string
	value := reflect.ValueOf(made)
	for i := range value.NumField() {
		if value.Field(i).IsNil() {
			quiet = append(quiet, value.Type().Field(i).Name)
		}
	}
	return quiet
}

// Name is the namespace this universe is, as it was created.
func (u *Universe) Name() string {
	if u == nil {
		return ""
	}
	return u.name
}

// Store is the attestations of this namespace.
func (u *Universe) Store() ats.AttestationStore {
	if u == nil {
		return nil
	}
	return u.made.Store
}

// Watchers are the watcher declarations of this namespace.
func (u *Universe) Watchers() storage.Watchers {
	if u == nil {
		return nil
	}
	return u.made.Watchers
}

// Schedules are the scheduled jobs of this namespace.
func (u *Universe) Schedules() *schedule.Store {
	if u == nil {
		return nil
	}
	return u.made.Schedules
}

// Serving is the node's universes when it runs one: the default, made of what
// it is given. A backend that runs a single namespace says so this way.
func Serving(made Made) (*Held, error) {
	universe, err := NewUniverse(auth.NamespaceDefault, made)
	if err != nil {
		return nil, err
	}
	held := &Held{}
	held.SetDefault(universe)
	return held, nil
}
