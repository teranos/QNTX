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
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok {
		return s.held.Served(), nil
	}

	if !admitted.ReachesAStore() {
		return nil, namespaces.ReachesNothing{}
	}

	return s.held.Write(admitted, namespaceOf(admitted))
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
