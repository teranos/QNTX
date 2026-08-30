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

	namespace := namespaceOf(admitted)
	// The system namespace is not visible below SUPER (ADR-027), and a SUPER
	// token reaches what ROOT granted it. Both are above; everything else is not.
	if namespace == auth.NamespaceSystem &&
		admitted.Level != auth.LevelRoot && admitted.Level != auth.LevelSuper {
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

// errNamespaceNotServed names the namespace that was asked for.
type errNamespaceNotServed struct{ asked string }

func (e errNamespaceNotServed) Error() string {
	return "the node does not serve " + e.asked
}
