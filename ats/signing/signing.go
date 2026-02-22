// Package signing provides ed25519 signing and verification for attestations.
// Signatures bind attestations to node DIDs: every locally-created attestation
// is signed by the node's private key, and synced attestations are verified
// against the signer's public key extracted from their did:key.
package signing

import (
	"crypto/ed25519"
	"encoding/json"

	"github.com/mr-tron/base58"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// Signer holds the node's signing identity.
type Signer struct {
	PrivateKey ed25519.PrivateKey
	DID        string
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

	as.Signature = ed25519.Sign(s.PrivateKey, canonical)
	as.SignerDID = s.DID
	return nil
}

// Verify checks that the attestation's signature is valid for its content.
// Returns nil if the signature is valid or the attestation is unsigned.
// Returns an error if the signature is present but invalid.
func Verify(as *types.As) error {
	// TODO(#583): Reject unsigned attestations once all nodes sign
	if len(as.Signature) == 0 {
		return nil // unsigned — backward compat
	}

	if as.SignerDID == "" {
		return errors.Newf("attestation %s has signature but no signer DID", as.ID)
	}

	pub, err := DecodeDIDKey(as.SignerDID)
	if err != nil {
		return errors.Wrapf(err, "failed to decode signer DID %s for attestation %s", as.SignerDID, as.ID)
	}

	canonical, err := CanonicalJSON(as)
	if err != nil {
		return errors.Wrapf(err, "failed to produce canonical JSON for verification of %s", as.ID)
	}

	if !ed25519.Verify(pub, canonical, as.Signature) {
		return errors.Newf("invalid signature on attestation %s from %s", as.ID, as.SignerDID)
	}

	return nil
}

// CanonicalJSON produces a deterministic byte representation of the attestation
// for signing. Excludes created_at (set by receiving DB, differs across nodes)
// and the signature fields themselves.
//
// Go's json.Marshal produces deterministic output for structs (field order is
// declaration order per the Go spec).
func CanonicalJSON(as *types.As) ([]byte, error) {
	wire := struct {
		ID         string                 `json:"id"`
		Subjects   []string               `json:"subjects"`
		Predicates []string               `json:"predicates"`
		Contexts   []string               `json:"contexts"`
		Actors     []string               `json:"actors"`
		Timestamp  int64                  `json:"timestamp"`
		Source     string                 `json:"source"`
		Attributes map[string]interface{} `json:"attributes,omitempty"`
	}{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
	}
	return json.Marshal(wire)
}

// DecodeDIDKey extracts the ed25519 public key from a did:key:z... identifier.
// Reverses the encoding: did:key:z + base58btc(0xed 0x01 + 32-byte pubkey)
func DecodeDIDKey(did string) (ed25519.PublicKey, error) {
	const prefix = "did:key:z"
	if len(did) < len(prefix) || did[:len(prefix)] != prefix {
		return nil, errors.Newf("invalid did:key format: %s", did)
	}

	decoded, err := base58.Decode(did[len(prefix):])
	if err != nil {
		return nil, errors.Wrapf(err, "failed to base58-decode did:key %s", did)
	}

	// Expect multicodec prefix 0xed 0x01 followed by 32-byte ed25519 public key
	if len(decoded) != 34 {
		return nil, errors.Newf("unexpected decoded length %d for did:key %s (expected 34)", len(decoded), did)
	}
	if decoded[0] != 0xed || decoded[1] != 0x01 {
		return nil, errors.Newf("unexpected multicodec prefix [%x %x] for did:key %s", decoded[0], decoded[1], did)
	}

	return ed25519.PublicKey(decoded[2:]), nil
}
