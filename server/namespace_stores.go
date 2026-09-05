package server

import (
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// NamespaceOpener opens the attestation store for one namespace. A backend that
// keeps namespaces has one; the rest keep a single universe and set none.
type NamespaceOpener interface {
	OpenNamespace(name string) (ats.AttestationStore, error)
	// CloseNamespace stops the flush loop opening one started and releases the
	// handle. What was written stays written — this is the process letting go,
	// not the bytes going anywhere.
	//
	// Without it a namespace that is gone keeps a handle open and keeps
	// flushing, and a flush writes its prefix back (create_dir_all in
	// crates/ats-duckdb/src/lib.rs). The node would recreate what it was told
	// to stop holding.
	CloseNamespace(name string) error
}

// SetNamespaceOpener gives the server a way to reach a namespace created after
// it started. Without it only the two stores opened at boot are reachable.
func (s *QNTXServer) SetNamespaceOpener(opener NamespaceOpener) {
	s.namespaceOpener = opener
}

// namespaceStores is the open store per namespace, filled as requests ask for
// them — a namespace created through the UI is usable without a restart.
//
// Keyed by slug and not by what was asked for: "Clean" and "clean" are one
// namespace, so they are one open store and never two.
type namespaceStores struct {
	mu   sync.Mutex
	open map[string]ats.AttestationStore
}

// namespaceNamed is the store's own name for the namespace something reaches
// by slug. The door says "clean", the store says "Clean", and the store is
// opened under the name the store keeps.
//
// Two namespaces with one slug is refused rather than picked between: which
// universe somebody lands in would be decided by the order a list came back in.
func namespaceNamed(known []storage.Namespace, asked string) (string, error) {
	reachedBy := slug.Of(asked)
	found := ""
	for _, namespace := range known {
		if slug.Of(namespace.Name) != reachedBy {
			continue
		}
		if found != "" {
			return "", errNamespaceAmbiguous{asked: asked, one: found, other: namespace.Name}
		}
		found = namespace.Name
	}
	if found == "" {
		return "", errNamespaceNotServed{asked: asked}
	}
	return found, nil
}

// storeIn returns the attestation store for one namespace, opening it the first
// time it is asked for.
func (s *QNTXServer) storeIn(namespace string) (ats.AttestationStore, error) {
	switch namespace {
	case auth.NamespaceDefault:
		return s.atsStore, nil
	case auth.NamespaceSystem:
		if s.systemStore == nil {
			return nil, errNamespaceNotServed{asked: namespace}
		}
		return s.systemStore, nil
	}
	if store, open := s.alreadyOpen(namespace); open {
		return store, nil
	}
	if s.namespaceOpener == nil || s.namespaces == nil {
		return nil, errNamespaceNotServed{asked: namespace}
	}

	// Opening writes a prefix at the location on the first flush, so a name
	// nobody created would become the namespace it misspelled.
	known, err := s.namespaces.List()
	if err != nil {
		return nil, errors.Wrapf(err, "cannot tell whether %s exists", namespace)
	}
	// A door is keyed by slug and a namespace keeps the name it was created
	// with, so what opens the store is the store's name and never the key.
	name, err := namespaceNamed(known, namespace)
	if err != nil {
		return nil, err
	}
	return s.openNamespace(name)
}

// alreadyOpen is the handle this process is already holding for a namespace.
func (s *QNTXServer) alreadyOpen(namespace string) (ats.AttestationStore, bool) {
	s.stores.mu.Lock()
	defer s.stores.mu.Unlock()
	store, open := s.stores.open[slug.Of(namespace)]
	return store, open
}

// openNamespace opens the store and files it under the slug it is reached by.
func (s *QNTXServer) openNamespace(name string) (ats.AttestationStore, error) {
	s.stores.mu.Lock()
	defer s.stores.mu.Unlock()
	// Asked again under the lock: two requests can reach a namespace at once,
	// and opening it twice would leave two buffers flushing one prefix.
	if store, open := s.stores.open[slug.Of(name)]; open {
		return store, nil
	}

	store, err := s.namespaceOpener.OpenNamespace(name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the attestation store for %s", name)
	}
	if s.stores.open == nil {
		s.stores.open = map[string]ats.AttestationStore{}
	}
	s.stores.open[slug.Of(name)] = store
	s.logger.Infow("Opened a namespace", "namespace", name, "reached_by", slug.Of(name))
	return store, nil
}

// closeIn drops the open store for one namespace: out of the cache, and the
// flush loop behind it stopped.
//
// storeIn answers from the cache before it asks whether the namespace is still
// there, so a handle nothing evicts keeps serving a namespace that is gone.
// Evicting alone is not enough either — the flush loop holds its own reference
// and writes the prefix back on the next tick.
//
// A namespace that was never opened is not an error. Nothing is holding it.
func (s *QNTXServer) closeIn(namespace string) error {
	reachedBy := slug.Of(namespace)

	s.stores.mu.Lock()
	_, held := s.stores.open[reachedBy]
	delete(s.stores.open, reachedBy)
	s.stores.mu.Unlock()

	if !held || s.namespaceOpener == nil {
		return nil
	}
	if err := s.namespaceOpener.CloseNamespace(namespace); err != nil {
		return errors.Wrapf(err, "the store for %s was evicted and would not close", namespace)
	}
	s.logger.Infow("Closed a namespace", "namespace", namespace, "reached_by", reachedBy)
	return nil
}
