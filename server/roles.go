package server

import (
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/errors"
)

// mayGrantEvery asks, for every role a grant line names and the one namespace
// it holds in, whether this admission may grant it. Who may is what the
// role's reach lines said after `by`, which the reach package holds beside
// the mux; ROOT always may. A line naming two roles is granted only by
// somebody who may grant both.
func (s *QNTXServer) mayGrantEvery(admitted auth.Admission, predicates, contexts []string) bool {
	if len(contexts) != 1 {
		return s.authHandler.MayGrantRoles(admitted)
	}
	for _, role := range rolesNamed(predicates) {
		var granters []string
		if s.served != nil {
			granters = s.served.Granters(role)
		}
		if !s.authHandler.MayGrant(admitted, contexts[0], granters) {
			return false
		}
	}
	return true
}

// rolesNamed is the roles on a grant line: every predicate beside the one
// that says granted or revoked.
func rolesNamed(predicates []string) []string {
	var roles []string
	for _, predicate := range predicates {
		if predicate == auth.PredicateRoleGranted || predicate == auth.PredicateRoleRevoked {
			continue
		}
		roles = append(roles, strings.ToUpper(predicate))
	}
	return roles
}

// WordLines is every WRITE and READ line in the system store: what a role
// may say. Found by subject, one at a time, the way the role lines are.
func (r roleLines) WordLines() ([]auth.WordLine, error) {
	store, err := r.s.held.Read(auth.NamespaceSystem)
	if err != nil {
		return nil, errors.Wrapf(err, "what the roles may say is kept in %s, which is not open",
			auth.NamespaceSystem)
	}
	var lines []auth.WordLine
	for _, subject := range []string{auth.SubjectWrite, auth.SubjectRead} {
		found, err := store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{subject},
			Limit:    storage.MaxAttestationLimit,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read the %s lines", subject)
		}
		for _, as := range found {
			if line, ok := auth.AsWordLine(as); ok {
				lines = append(lines, line)
			}
		}
	}
	return lines, nil
}

// runtime is the store's reach lines, read for every Open and Reopen. The
// const table is the floor; these add roles to it. A line is found by its
// subject, REACH, the way the const's lines are about REACH: its predicates
// are the paths and its contexts are the roles. A line the store holds that
// the reach package will not read is said and skipped: a bad line in the store
// is not a reason for the node to serve nothing.
func (s *QNTXServer) runtime() reach.Runtime {
	runtime := reach.Runtime{}
	if s.authHandler != nil {
		runtime.IsRoot = s.authHandler.IsRoot
	}
	// A backend that keeps no system store keeps no lines: the const serves
	// alone, and that is not an error to say.
	if !s.held.KeepsSystem() {
		return runtime
	}
	store, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		s.logger.Errorw("the store's reach lines were not read; the const table serves alone",
			"error", err)
		return runtime
	}
	found, err := store.GetAttestations(ats.AttestationFilter{
		Subjects: []string{reach.Subject},
		Limit:    storage.MaxAttestationLimit,
	})
	if err != nil {
		s.logger.Errorw("the store's reach lines were not read; the const table serves alone",
			"error", err)
		return runtime
	}
	for _, as := range found {
		line, err := reach.ReadLine(as.Subjects, as.Predicates, as.Contexts, as.Actors, as.Timestamp)
		if err != nil {
			s.logger.Errorw("a stored reach line is not served", "id", as.ID, "error", err)
			continue
		}
		runtime.Lines = append(runtime.Lines, line)
	}
	return runtime
}

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
	store, err := r.s.held.Read(auth.NamespaceSystem)
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
