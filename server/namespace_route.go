package server

import (
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
)

// storeFor returns the attestation store this request acts in.
//
// Namespaces are their own universes and nothing crosses (ADR-026), so where a
// request acts is a fact about the caller. Nothing a request carries names it.
func (s *QNTXServer) storeFor(r *http.Request) (ats.AttestationStore, error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	u, err := s.universeFor(admitted, gated)
	if err != nil {
		return nil, err
	}
	return u.Store(), nil
}

// universeFor is the universe an admission acts in, whole.
//
// A socket outlives the request that opened it, so a connection holds what it
// was admitted as and asks here. storeFor is this and then its attestations.
func (s *QNTXServer) universeFor(admitted auth.Admission, gated bool) (*namespaces.Universe, error) {
	// Not gated is a node running without auth, where every caller is the one
	// caller. The served universe is what such a node has to give.
	if !gated {
		return s.held.ServedUniverse(), nil
	}
	if !admitted.ReachesAStore() {
		return nil, namespaces.ReachesNothing{}
	}
	return s.held.Universe(admitted, namespaceOf(admitted))
}

// namespaceOf is the universe this caller is in.
//
// A token names where it may act when it is minted, and acts there. A session
// acts in the namespace of the door its person registered at (ADR-032). A
// session that came in by no door names none, and that is the default.
func namespaceOf(admitted auth.Admission) string {
	if len(admitted.Namespaces) == 1 {
		return admitted.Namespaces[0]
	}
	return auth.NamespaceDefault
}
