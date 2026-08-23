package server

import (
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
)

// servedNamespace is the namespace s.atsStore was opened for, once, at startup.
// It is what any request against that store actually reads and writes.
const servedNamespace = auth.NamespaceDefault

// storeFor returns the attestation store a caller may use, or an error naming
// why not. A token carries the namespace it was minted for (ADR-025), nothing
// routed it, and it read and wrote another while being told it had worked.
func (s *QNTXServer) storeFor(r *http.Request) (ats.AttestationStore, error) {
	caller, ok := auth.CallerFrom(r.Context())
	if !ok || caller.Namespace == "" || caller.Namespace == servedNamespace {
		return s.atsStore, nil
	}
	// What the boundary costs until a store is resolved per caller rather than
	// at construction. It is the only answer that writes nowhere else.
	return nil, errNamespaceNotServed{asked: caller.Namespace}
}

// errNamespaceNotServed names both namespaces, because a caller told only that
// something is unavailable cannot tell a typo from an unimplemented route.
type errNamespaceNotServed struct{ asked string }

func (e errNamespaceNotServed) Error() string {
	return "the node does not serve " + e.asked
}
