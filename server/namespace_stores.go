package server

import (
	"github.com/teranos/QNTX/server/namespaces"
)

// NamespaceOpener opens the attestation store for one namespace. A backend that
// keeps namespaces has one; the rest keep a single universe and set none.
type NamespaceOpener = namespaces.Opener

// SetNamespaceOpener gives the server a way to reach a namespace created after
// it started. Without it only the two stores opened at boot are reachable.
func (s *QNTXServer) SetNamespaceOpener(opener NamespaceOpener) {
	s.held.SetOpener(opener)
}
