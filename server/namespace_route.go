package server

import (
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
)

// storeFor returns the attestation store this request acts in.
//
// Namespaces are their own universes and nothing crosses (ADR-026), so where a
// request acts is a fact about the caller. Nothing a request carries names it.
func (s *QNTXServer) storeFor(r *http.Request) (ats.AttestationStore, error) {
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok {
		return s.atsStore, nil
	}

	if !admitted.ReachesAStore() {
		return nil, errReachesNothing{}
	}

	namespace := namespaceOf(admitted)
	if namespace == auth.NamespaceSystem && !admitted.MaySeeSystem() {
		return nil, errNamespaceNotServed{asked: namespace}
	}
	return s.storeIn(namespace)
}

// namespaceOf is the universe this caller is in.
//
// A token names where it may act when it is minted, and acts there. A session
// acts in the default namespace until being in one is something a person does.
func namespaceOf(admitted auth.Admission) string {
	if len(admitted.Namespaces) == 1 {
		return admitted.Namespaces[0]
	}
	return auth.NamespaceDefault
}

// errReachesNothing is the answer for a rung that holds no store. It names no
// namespace: a caller who reaches nothing learns nothing about what is there.
type errReachesNothing struct{}

func (errReachesNothing) Error() string { return "this admission reaches no store" }

// errNamespaceNotServed names the namespace that was asked for.
type errNamespaceNotServed struct{ asked string }

func (e errNamespaceNotServed) Error() string {
	return "the node does not serve " + e.asked
}
