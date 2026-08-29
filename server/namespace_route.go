package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
)

// servedNamespace is the namespace s.atsStore was opened for, once, at startup.
// It is what any request against that store actually reads and writes.
const servedNamespace = auth.NamespaceDefault

// storeFor returns the attestation store this request may use. A token carries
// the namespace it was minted for (ADR-025), and one store is open, so any
// other namespace is refused rather than served by the one that is.
func (s *QNTXServer) storeFor(r *http.Request) (ats.AttestationStore, error) {
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok || len(admitted.Namespaces) == 0 {
		return s.atsStore, nil
	}
	// A token may name several. One store is open, so it is served when the
	// token names it and refused when it does not.
	if slices.Contains(admitted.Namespaces, servedNamespace) {
		return s.atsStore, nil
	}
	return nil, errNamespaceNotServed{asked: strings.Join(admitted.Namespaces, ", ")}
}

// errNamespaceNotServed names the namespace that was asked for.
type errNamespaceNotServed struct{ asked string }

func (e errNamespaceNotServed) Error() string {
	return "the node does not serve " + e.asked
}
