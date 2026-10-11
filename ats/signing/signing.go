// Package signing provides ed25519 signing and verification for attestations.
// Signatures bind attestations to node DIDs: every locally-created attestation
// is signed by the node's private key, and synced attestations are verified
// against the signer's public key extracted from their did:key.
package signing

import (
	"crypto/ed25519"
	"encoding/json"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	errors "github.com/teranos/sacred-error"
)

// ErrInvalidSignature is a signature that does not stand for what it came with
// — not over its bytes, or from a signer DID that names no key: the writer's
// mistake, where every other error from Verify is this node's.
var ErrInvalidSignature = errors.New("invalid signature")

// Signer holds the node's signing identity.
// PrivateKey is unexported to prevent accidental logging/exposure.
type Signer struct {
	privateKey ed25519.PrivateKey
	DID        string
}

// NewSigner creates a new Signer with the given private key and DID.
func NewSigner(privateKey ed25519.PrivateKey, did string) *Signer {
	return &Signer{
		privateKey: privateKey,
		DID:        did,
	}
}

// Sign populates Signature and SignerDID on the attestation.
// Only signs if the attestation is not already signed.
func (s *Signer) Sign(as *types.As) error {
	if len(as.Signature) > 0 {
		return nil // already signed (e.g. received via sync)
	}

	canonical, err := CanonicalJSON(as)
	if err != nil {
		return errors.Wrapf(err, "failed to produce canonical JSON for %s", as.ID)
	}

	as.Signature = ed25519.Sign(s.privateKey, canonical)
	as.SignerDID = s.DID
	return nil
}

// Claim is what an attestation says of who signed it.
type Claim int

const (
	// Unsigned carries neither a signature nor a signer: whoever stores it
	// signs it.
	Unsigned Claim = iota + 1
	// Signed carries a signature and the did:key of the signer it names, and
	// is stored as it was signed.
	Signed
)

// Claimed is what a signature and a signer DID claim together. One without the
// other is refused: a signer named for nothing signed, or a signature nobody
// is named for.
func Claimed(signature []byte, signerDID string) (Claim, error) {
	signs, names := len(signature) > 0, signerDID != ""
	switch {
	case signs && names:
		return Signed, nil
	case !signs && !names:
		return Unsigned, nil
	}
	return 0, errors.Wrapf(ErrInvalidSignature,
		"a signature of %d bytes and signer DID %q: each is half of a signature", len(signature), signerDID)
}

// Verify checks that the attestation's signature is valid for its content.
// Returns nil if the signature is valid or the attestation is unsigned.
// Returns an error if the signature is present but invalid.
func Verify(as *types.As) error {
	claim, err := Claimed(as.Signature, as.SignerDID)
	if err != nil {
		return errors.Wrapf(err, "attestation %s", as.ID)
	}
	// TODO(#583): Reject unsigned attestations once all nodes sign
	if claim == Unsigned {
		return nil // unsigned — backward compat
	}

	pub, err := DecodeDIDKey(as.SignerDID)
	if err != nil {
		return errors.WithSecondaryError(
			errors.Wrapf(ErrInvalidSignature, "signer DID %s on attestation %s does not decode", as.SignerDID, as.ID),
			err)
	}

	canonical, err := CanonicalJSON(as)
	if err != nil {
		return errors.Wrapf(err, "failed to produce canonical JSON for verification of %s", as.ID)
	}

	if !ed25519.Verify(pub, canonical, as.Signature) {
		return errors.Wrapf(ErrInvalidSignature, "attestation %s from %s", as.ID, as.SignerDID)
	}

	return nil
}

// CanonicalJSON is the bytes a signature is over. Excludes created_at (set by
// the receiving DB, differs across nodes) and the signature fields themselves.
//
// crates/ats-sign writes them, so a browser signing and a node verifying reach
// the same bytes; testdata/canonical.json holds what both are held to. Nil
// slices are written as empty, for consistency with the sync representation.
func CanonicalJSON(as *types.As) ([]byte, error) {
	nilToEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	wire, err := json.Marshal(struct {
		ID         string         `json:"id"`
		Subjects   []string       `json:"subjects"`
		Predicates []string       `json:"predicates"`
		Contexts   []string       `json:"contexts"`
		Actors     []string       `json:"actors"`
		Timestamp  int64          `json:"timestamp"`
		Source     string         `json:"source"`
		Attributes map[string]any `json:"attributes,omitempty"`
	}{
		ID:         as.ID,
		Subjects:   nilToEmpty(as.Subjects),
		Predicates: nilToEmpty(as.Predicates),
		Contexts:   nilToEmpty(as.Contexts),
		Actors:     nilToEmpty(as.Actors),
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal attestation %s for signing", as.ID)
	}
	return canonical(wire)
}

// DecodeDIDKey extracts the ed25519 public key from a did:key:z... identifier.
func DecodeDIDKey(did string) (ed25519.PublicKey, error) {
	return access.DecodeUserDID(did)
}
