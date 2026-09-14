package server

import (
	"net/http"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// footing is whether an admission may stand in a namespace, asked at the same
// door a write goes through. Not served is 404, switched off is 409.
func (s *QNTXServer) footing(admitted auth.Admission, namespace string) (int, string) {
	_, err := s.held.Universe(admitted, namespace)
	if err == nil {
		return 0, ""
	}
	var off namespaces.Disabled
	if errors.As(err, &off) {
		return http.StatusConflict, err.Error()
	}
	var notServed namespaces.NotServed
	var ambiguous namespaces.Ambiguous
	if errors.As(err, &notServed) || errors.As(err, &ambiguous) {
		return http.StatusNotFound, err.Error()
	}
	return http.StatusInternalServerError, "could not tell whether " + namespace + " may be stood in: " + err.Error()
}

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
	return s.held.Universe(admitted, s.namespaceOf(admitted))
}

// namespaceOf is the universe this caller is in.
//
// A token names where it may act when it is minted, and acts there. A session
// acts in the namespace of the door its person registered at (ADR-032).
//
// A session that came in by no door reaches every namespace the node serves,
// and which one it acts in is where the person is standing — the rectangle in
// the namespaces bar. Standing nowhere yet is the default project.
//
// The reading is auth.StandingIn's, which is also what GET /i/ answers, so the
// rectangle a person sees is the namespace their writes land in.
func (s *QNTXServer) namespaceOf(admitted auth.Admission) string {
	return auth.StandingIn(admitted, s.authHandler.StandingOf(admitted.UserID))
}
