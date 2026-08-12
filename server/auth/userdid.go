package auth

import (
	"crypto/ed25519"

	"github.com/mr-tron/base58"
	"github.com/teranos/errors"
)

// didKeyPrefix is the framing shared with the node DID (ats/signing):
// did:key:z + base58btc(0xed 0x01 + 32-byte ed25519 key).
const didKeyPrefix = "did:key:z"

// EncodeDIDKey renders an ed25519 public key as a did:key identifier.
func EncodeDIDKey(pub ed25519.PublicKey) string {
	buf := make([]byte, 2+len(pub))
	buf[0] = 0xed
	buf[1] = 0x01
	copy(buf[2:], pub)
	return didKeyPrefix + base58.Encode(buf)
}

// DecodeUserDID extracts the ed25519 public key a did:key names.
func DecodeUserDID(did string) (ed25519.PublicKey, error) {
	if len(did) < len(didKeyPrefix) || did[:len(didKeyPrefix)] != didKeyPrefix {
		return nil, errors.Newf("user identity %q is not a did:key", did)
	}
	decoded, err := base58.Decode(did[len(didKeyPrefix):])
	if err != nil {
		return nil, errors.Wrapf(err, "failed to base58-decode did:key %s", did)
	}
	if len(decoded) != 2+ed25519.PublicKeySize {
		return nil, errors.Newf("did:key %s decodes to %d bytes, expected %d", did, len(decoded), 2+ed25519.PublicKeySize)
	}
	if decoded[0] != 0xed || decoded[1] != 0x01 {
		return nil, errors.Newf("did:key %s is not ed25519 (multicodec %x %x)", did, decoded[0], decoded[1])
	}
	return ed25519.PublicKey(decoded[2:]), nil
}

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
