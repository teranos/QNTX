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

// identitiesGovern reports whether this deployment names who may log in. When
// nothing is listed, a passkey answers to itself and nothing else — the state
// an install stays in until someone puts an identity in am.toml.
func (h *Handler) identitiesGovern() bool {
	return len(h.identities.roots()) > 0
}

// stillAdmitted re-checks an identity against am.toml at the moment it is used.
// A passkey carries the account it was enrolled under rather than a decision,
// so removing the account from the list is what revokes the passkey.
func (h *Handler) stillAdmitted(identity string) bool {
	return slices.Contains(h.identities.roots(), identity)
}

// admits reports whether a DID or any account it verifiably holds is listed.
// A did:key entry needs no binding — it is a key, and the signature already
// proved possession.
func (h *Handler) admits(did string, peerPubkey ed25519.PublicKey, presented []SignedBinding) (string, bool) {
	if slices.Contains(h.identities.roots(), did) {
		return did, true
	}
	signers := h.identities.trustedSigners()
	for _, binding := range presented {
		if err := verifyBinding(binding, peerPubkey, signers); err != nil {
			h.logger.Infow("binding refused", "error", err)
			continue
		}
		if slices.Contains(h.identities.roots(), binding.Claim.CanonicalID) {
			return binding.Claim.CanonicalID, true
		}
	}
	return "", false
}
