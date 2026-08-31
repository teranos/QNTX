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
	// A display_name settles once and can never be taken back, so when it was
	// settled is a fact the owner can go and look at.
	PredicateNamed = "identity:named"
	// Someone proved an account at a provider. Not that they were let in —
	// admission is asked separately and this is the arriving.
	PredicateRegistered = "identity:registered"
)

// Predicates for a credential's life. A token outlives the session that minted
// it, so both ends of that life are recorded.
const (
	PredicateMinted  = "token:minted"
	PredicateRevoked = "token:revoked"
	PredicateEnabled = "token:enabled"
	// What a token may touch is changed on the token it already is (TOKATTEST), so
	// the record only ever says what it may do now. The change is the history.
	PredicateScoped = "token:scoped"
)

// A dependency the node asked and got no answer from: a store, a provider,
// crypto/rand. The caller is blameless and the deployment is not well, which is
// a fact about the node rather than about one request.
const PredicateUnanswered = "node:unanswered"

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
		// The attributes are the whole of what this was going to say, and a
		// store refusing them is exactly when they are worth keeping. Naming
		// the refusal without them records that something was lost, not what.

		// Error rather than Warn: a node that cannot write down who it admitted
		// and who it turned away has stopped being able to account for itself.
		h.logger.Errorw("admission not attested: the store refused it",
			"predicate", predicate, "subject", subject,
			"attributes", attrs, "error", err)
	}
}

// attestRegistration records that somebody arrived at a door and proved an
// account there.
//
// The ceremony is open by design: linking happens before anyone can log in, so
// it cannot be gated on a session. The door is what bounds it instead — a
// ceremony that reached none is not an arrival anywhere, and writes nothing.
func (h *Handler) attestRegistration(providerID string, acct account, door string) {
	// FIXME: two silent returns. An arrival that was not recorded leaves no
	// trace of not being recorded, which is the observability this drops.
	if acct.CanonicalID == "" || door == "" {
		return
	}

	attrs := map[string]any{"provider": providerID, "door": door}
	// The provider decides what it hands over. An empty handle written down
	// would say it named nobody, which is not the same as not being asked.
	if acct.Handle != "" {
		attrs["handle"] = acct.Handle
	}
	h.attest(PredicateRegistered, acct.CanonicalID, attrs)
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
