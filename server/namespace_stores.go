package server

import (
	"slices"
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
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
type namespaceStores struct {
	mu   sync.Mutex
	open map[string]ats.AttestationStore
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
	if store, ok := s.stores.open[namespace]; ok {
		return store, nil
	}

	// Opening writes a prefix at the location on the first flush, so a name
	// nobody created would become the namespace it misspelled.
	known, err := s.namespaces.List()
	if err != nil {
		return nil, errors.Wrapf(err, "cannot tell whether %s exists", namespace)
	}
	if !slices.ContainsFunc(known, func(n storage.Namespace) bool { return n.Name == namespace }) {
		return nil, errNamespaceNotServed{asked: namespace}
	}

	store, err := s.namespaceOpener.OpenNamespace(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open the attestation store for %s", namespace)
	}
	if s.stores.open == nil {
		s.stores.open = map[string]ats.AttestationStore{}
	}
	s.stores.open[namespace] = store
	s.logger.Infow("Opened a namespace", "namespace", namespace)
	return store, nil
}
