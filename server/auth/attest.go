package auth

import (
	"crypto/ed25519"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
)

// Predicates for what happens to an identity at the door. Admission is a fact
// about the deployment, not a line in a log that rotates away.
const (
	PredicateLoggedIn  = "identity:admitted"
	PredicateRefused   = "identity:refused"
	PredicateLoggedOut = "identity:released"
)

// Attestor is the write half of the attestation store. Narrow on purpose: the
// auth package records and never reads back.
type Attestor interface {
	CreateAttestation(as *types.As) error
}

// SetAttestor hands the handler somewhere to record admissions. Nil until the
// store is up, and a nil attestor records nothing rather than failing a login.
func (h *Handler) SetAttestor(a Attestor) {
	h.attestor = a
}

// attest records one thing that happened at the door. It never fails the
// request it describes — a login that worked is not undone by failing to write
// it down — so a store that refuses says so in the log and nowhere else.
func (h *Handler) attest(predicate, subject string, attrs map[string]any) {
	if h.attestor == nil {
		return
	}

	// The deployment talking about itself, so it belongs to the namespace that
	// is not anyone's.
	id, err := identity.GenerateASUID("AS", subject, predicate, NamespaceSystem)
	if err != nil {
		h.logger.Warnw("admission not attested: could not mint an id",
			"predicate", predicate, "subject", subject, "error", err)
		return
	}

	actor := h.nodeDIDOrUnknown()
	now := time.Now()
	as := &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{NamespaceSystem},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     actor,
		Attributes: attrs,
		CreatedAt:  now,
	}
	if err := h.attestor.CreateAttestation(as); err != nil {
		h.logger.Warnw("admission not attested: the store refused it",
			"predicate", predicate, "subject", subject, "error", err)
	}
}

// nodeDIDOrUnknown names who is doing the attesting. The node signs bindings
// with this key, so it is the identity the deployment answers as.
func (h *Handler) nodeDIDOrUnknown() string {
	if h.nodeKey == nil {
		return "did:key:unknown"
	}
	pub, ok := h.nodeKey.Public().(ed25519.PublicKey)
	if !ok {
		return "did:key:unknown"
	}
	return EncodeDIDKey(pub)
}
