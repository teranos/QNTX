package auth

import (
	"crypto/ed25519"

	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/errors"
)

// EncodeDIDKey renders an ed25519 public key as a did:key identifier.
func EncodeDIDKey(pub ed25519.PublicKey) string { return access.EncodeDIDKey(pub) }

// DecodeUserDID extracts the ed25519 public key a did:key names.
func DecodeUserDID(did string) (ed25519.PublicKey, error) { return access.DecodeUserDID(did) }

// VerifyUserDID checks that whoever presented this DID holds its private key.
// The PRF seed never leaves the browser, so possession is the only thing the
// server can check — without it a DID is a claim anyone could make about anyone.
func VerifyUserDID(did string, challenge, signature []byte) error {
	pub, err := DecodeUserDID(did)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, challenge, signature) {
		return errors.Newf("signature over the ceremony challenge does not verify for %s", did)
	}
	return nil
}
