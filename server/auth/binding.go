package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"

	"github.com/teranos/errors"
)

// SignedBinding is laye's wire shape (crates/me). A binding says "this peer
// key belongs to this account", and it is worth exactly as much as the key
// that signed it.
type SignedBinding struct {
	Claim struct {
		PeerPubkeyHex string  `json:"peer_pubkey_hex"`
		Provider      string  `json:"provider"`
		CanonicalID   string  `json:"canonical_id"`
		Handle        *string `json:"handle"`
		IssuedAt      uint64  `json:"issued_at"`
	} `json:"claim"`
	SignatureHex    string `json:"signature_hex"`
	SignerPubkeyHex string `json:"signer_pubkey_hex"`
}

// canonicalBytes reproduces laye-binding/v1 from crates/me/src/lib.rs. Both
// sides must render it identically or every signature fails.
func (b SignedBinding) canonicalBytes() []byte {
	handle := ""
	if b.Claim.Handle != nil {
		handle = *b.Claim.Handle
	}
	return []byte(fmt.Sprintf("laye-binding/v1|%s|%s|%s|%s|%d",
		b.Claim.PeerPubkeyHex, b.Claim.Provider, b.Claim.CanonicalID, handle, b.Claim.IssuedAt))
}

// verifyBinding is the anchor check. laye's own verify() reads the signing key
// out of the message, which proves only that the message is self-consistent.
// Trusting the signer is what makes a claim about an account mean anything.
func verifyBinding(b SignedBinding, peerPubkey ed25519.PublicKey, trustedSigners []string) error {
	claimed, err := hex.DecodeString(b.Claim.PeerPubkeyHex)
	if err != nil {
		return errors.Wrapf(err, "binding for %s has an unreadable peer pubkey", b.Claim.CanonicalID)
	}
	if !ed25519.PublicKey(claimed).Equal(peerPubkey) {
		return errors.Newf("binding for %s is about another key than the one that signed in", b.Claim.CanonicalID)
	}

	if !slices.Contains(trustedSigners, b.SignerPubkeyHex) {
		return errors.Newf("binding for %s is signed by %s, which is not in auth.binding_signers", b.Claim.CanonicalID, b.SignerPubkeyHex)
	}

	signer, err := hex.DecodeString(b.SignerPubkeyHex)
	if err != nil {
		return errors.Wrapf(err, "binding for %s has an unreadable signer pubkey", b.Claim.CanonicalID)
	}
	if len(signer) != ed25519.PublicKeySize {
		return errors.Newf("signer pubkey for %s is %d bytes, expected %d", b.Claim.CanonicalID, len(signer), ed25519.PublicKeySize)
	}

	signature, err := hex.DecodeString(b.SignatureHex)
	if err != nil {
		return errors.Wrapf(err, "binding for %s has an unreadable signature", b.Claim.CanonicalID)
	}
	if !ed25519.Verify(ed25519.PublicKey(signer), b.canonicalBytes(), signature) {
		return errors.Newf("binding for %s does not verify against its signer", b.Claim.CanonicalID)
	}
	return nil
}

// identityLists is who may log in and whose bindings count, read on every
// login and rewritten whenever am.toml changes. Revocation that waits for a
// restart is revocation the operator has to remember to finish.
type identityLists struct {
	mu      sync.RWMutex
	root    []string
	signers []string
}

// set replaces both lists. The config watcher calls this, so a request in
// flight either sees the whole old pair or the whole new one.
func (l *identityLists) set(root, signers []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.root = slices.Clone(root)
	l.signers = slices.Clone(signers)
}

func (l *identityLists) roots() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.root
}

func (l *identityLists) trustedSigners() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.signers
}

// identitiesGovern reports whether this deployment names anyone at all. It is
// what /setup publishes about itself, never a gate: a node listing nobody
// admits nobody, so there is nothing for the empty case to permit.
func (h *Handler) identitiesGovern() bool {
	return len(h.identities.roots()) > 0
}

// stillAdmitted re-checks an identity against am.toml at the moment it is used.
// A passkey carries the account it was enrolled under rather than a decision,
// so removing the account from the list is what revokes the passkey.
func (h *Handler) stillAdmitted(identity string) bool {
	return h.levelOf(identity) != ""
}

// stillProven re-checks a half-admission at the moment a device answers it:
// the account is still listed, and the binding that reached it still
// verifies against a signer still in auth.binding_signers. stillAdmitted is
// handed a string and can only ask the list; this is handed what the string
// stood on and asks the whole question again. A half-admission with no
// binding was its own proof, a did:key route, and the list is all there is.
func (h *Handler) stillProven(half halfAdmission) error {
	if !h.stillAdmitted(half.identity) {
		return errors.Newf("%s is no longer listed in auth.root_identities", half.identity)
	}
	if half.binding == nil {
		return nil
	}
	if half.binding.Claim.CanonicalID != half.identity {
		return errors.Newf("the binding carried is for %s, not %s", half.binding.Claim.CanonicalID, half.identity)
	}
	peer, err := DecodeUserDID(half.did)
	if err != nil {
		return errors.Wrapf(err, "the half-admission for %s names a key that does not decode", half.identity)
	}
	return verifyBinding(*half.binding, peer, h.identities.trustedSigners())
}

// heldBindingStillCounts asks again about the binding a User's account was
// reached by (ADR-031): its signer is still in auth.binding_signers, its
// signature still verifies, and the key it is about is one this User holds.
// An account with no binding kept is a record from before they were, and
// there is nothing to ask. Asked where the User is already read — when a
// passkey answers — because a per-request read of the User store is a list
// of every User per request.
func (h *Handler) heldBindingStillCounts(u User, route string) error {
	for _, a := range u.Accounts {
		if a.CanonicalID != route || a.Binding == nil {
			continue
		}
		claimed, err := hex.DecodeString(a.Binding.Claim.PeerPubkeyHex)
		if err != nil {
			return errors.Wrapf(err, "the binding kept for %s has an unreadable peer pubkey", route)
		}
		if len(claimed) != ed25519.PublicKeySize {
			return errors.Newf("the binding kept for %s is about a %d-byte key, expected %d", route, len(claimed), ed25519.PublicKeySize)
		}
		if !u.HoldsKey(EncodeDIDKey(ed25519.PublicKey(claimed))) {
			return errors.Newf("the binding kept for %s is about a key User %s does not hold", route, u.ID)
		}
		return verifyBinding(*a.Binding, ed25519.PublicKey(claimed), h.identities.trustedSigners())
	}
	return nil
}

// levelOf is how much an identity is admitted at, read from what admits it.
//
// A level asserted where an admission is built is a level with no provenance:
// nothing says why it is that one, so nothing can say when it should be
// another. This is the one place that decides, and every gate asks it.
//
// auth.root_identities lists the ways one User is reached (ADR-030), and that
// User is ROOT (ADR-031). Empty is not a rung — it is the answer for an
// identity nothing admits, and the caller refuses on it.
func (h *Handler) levelOf(identity string) Level {
	if slices.Contains(h.identities.roots(), identity) {
		return LevelRoot
	}
	// Somebody who walked up to a door and made themselves. The node holds a
	// User for them because a provider vouched once, and the rung is read off
	// that User — still a level with provenance, from a different record.
	return h.publicLevelOf(identity)
}

// proves returns the bindings that verifiably say this key holds an account,
// listed or not.
//
// Verifying and deciding are two questions, and this is only the first: a
// signer this node trusts said so. Whether am.toml also names the account is
// what admits asks of the answer.
func (h *Handler) proves(peerPubkey ed25519.PublicKey, presented []SignedBinding) []SignedBinding {
	signers := h.identities.trustedSigners()
	vouched := make([]SignedBinding, 0, len(presented))
	for _, binding := range presented {
		if err := verifyBinding(binding, peerPubkey, signers); err != nil {
			h.logger.Infow("binding refused", "error", err)
			continue
		}
		vouched = append(vouched, binding)
	}
	return vouched
}

// admits reports whether a DID or any account it verifiably holds is listed.
// A did:key entry needs no binding — it is a key, and the signature already
// proved possession.
// The matched binding rides along so the caller can record which kind of thing
// let someone in. Nil means the route was the key itself.
func (h *Handler) admits(did string, vouched []SignedBinding) (string, *SignedBinding, bool) {
	if slices.Contains(h.identities.roots(), did) {
		return did, nil, true
	}
	for _, binding := range vouched {
		if slices.Contains(h.identities.roots(), binding.Claim.CanonicalID) {
			return binding.Claim.CanonicalID, &binding, true
		}
	}
	return "", nil, false
}
