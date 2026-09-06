package server

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// roleLines reads back the grants the attestation handler writes.
//
// The auth package records and never reads back (attest.go), so this is a
// second interface and it lives out here, against the store. A package that
// could read the store could hold a second answer about who may do what.
type roleLines struct{ s *QNTXServer }

// RoleLines is every granted and revoked line in one namespace.
//
// They are all in the system store whatever namespace they hold in, which is
// where the node keeps what it knows about itself. The namespace a line is
// about is its context.
func (r roleLines) RoleLines(namespace string) ([]auth.RoleLine, error) {
	store, err := r.s.storeIn(auth.NamespaceSystem)
	if err != nil {
		return nil, errors.Wrapf(err, "the roles held in %s are kept in %s, which is not open",
			namespace, auth.NamespaceSystem)
	}

	var lines []auth.RoleLine
	// One predicate at a time. A filter naming both is an `and` on some
	// backends, and no line is ever both.
	for _, predicate := range []string{auth.PredicateRoleGranted, auth.PredicateRoleRevoked} {
		found, err := store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{predicate},
			Contexts:   []string{namespace},
			// The store's own ceiling, said out loud. Newest first, so a
			// deployment past it loses the oldest lines rather than the ones
			// that decide.
			Limit: storage.MaxAttestationLimit,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read the %s lines in %s", predicate, namespace)
		}
		for _, as := range found {
			if line, ok := auth.AsRoleLine(as); ok {
				lines = append(lines, line)
			}
		}
	}
	return lines, nil
}
