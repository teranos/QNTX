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
	if s.namespaceOpener == nil || s.namespaces == nil {
		return nil, errNamespaceNotServed{asked: namespace}
	}

	s.stores.mu.Lock()
	defer s.stores.mu.Unlock()
	if store, ok := s.stores.open[slug.Of(namespace)]; ok {
		return store, nil
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
