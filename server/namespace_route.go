package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
)

// storeFor returns the attestation store this request acts in.
func (s *QNTXServer) storeFor(r *http.Request) (ats.AttestationStore, error) {
	admitted, ok := auth.AdmissionFrom(r.Context())
	if !ok {
		return s.atsStore, nil
	}

	namespace, err := namespaceOf(r, admitted)
	if err != nil {
		return nil, err
	}
	// system is not visible below SUPER (ADR-027), and a token is below it.
	if namespace == auth.NamespaceSystem && admitted.Level != auth.LevelSuper {
		return nil, errNamespaceNotServed{asked: namespace}
	}
	return s.storeIn(namespace)
}

// namespaceOf answers which namespace a request acts in. Naming several, a
// token says which one this request is: a write lands somewhere or nowhere.
func namespaceOf(r *http.Request, admitted auth.Admission) (string, error) {
	asked := strings.TrimSpace(r.URL.Query().Get("namespace"))
	reach := admitted.Namespaces

	// A session names none, which is every namespace the node serves.
	if len(reach) == 0 {
		if asked == "" {
			return auth.NamespaceDefault, nil
		}
		return asked, nil
	}

	if asked == "" {
		if len(reach) == 1 {
			return reach[0], nil
		}
		return "", errNamespaceUnsaid{reach: reach}
	}
	if !slices.Contains(reach, asked) {
		return "", errNamespaceNotServed{asked: asked}
	}
	return asked, nil
}

// errNamespaceNotServed names the namespace that was asked for.
type errNamespaceNotServed struct{ asked string }

func (e errNamespaceNotServed) Error() string {
	return "the node does not serve " + e.asked
}

// errNamespaceUnsaid is a caller that reaches several namespaces and did not
// say which one this is. It names them, so the next request can pick.
type errNamespaceUnsaid struct{ reach []string }

func (e errNamespaceUnsaid) Error() string {
	return "this token reaches " + strings.Join(e.reach, ", ") +
		" — name one with ?namespace="
}
