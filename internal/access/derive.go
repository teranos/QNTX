package access

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/teranos/errors"
)

// DeriveToken is the token a key holds for one purpose: the same raw and the
// same DID every time, so nothing more is kept for it to be the same tomorrow.
// The purpose is what it is derived for, written down once: agent:root.
func DeriveToken(from ed25519.PrivateKey, purpose string) (raw, did string, err error) {
	if len(from) != ed25519.PrivateKeySize {
		return "", "", errors.Newf("a token is derived from an ed25519 key, and this one is %d bytes", len(from))
	}
	if purpose == "" {
		return "", "", errors.New("a token is derived for a purpose, and this named none")
	}
	// The HMAC of the purpose under the key's own seed: a DID of its own, and
	// nothing given back of the key it came from.
	mac := hmac.New(sha256.New, from.Seed())
	if _, err := mac.Write([]byte("qntx:derived-token:" + purpose)); err != nil {
		return "", "", errors.Wrapf(err, "the seed of the token for %s was not derived", purpose)
	}
	seed := mac.Sum(nil)
	pub, isEd25519 := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !isEd25519 {
		return "", "", errors.Newf("the seed derived for %s has no ed25519 public half, so the token has no DID", purpose)
	}
	return TokenPrefix + hex.EncodeToString(seed), EncodeDIDKey(pub), nil
}
