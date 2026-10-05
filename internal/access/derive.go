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
	key, err := DeriveKey(from, purpose)
	if err != nil {
		return "", "", err
	}
	pub, isEd25519 := key.Public().(ed25519.PublicKey)
	if !isEd25519 {
		return "", "", errors.Newf("the seed derived for %s has no ed25519 public half, so the token has no DID", purpose)
	}
	return TokenPrefix + hex.EncodeToString(key.Seed()), EncodeDIDKey(pub), nil
}

// DeriveKey is the key a derived token is the seed of: what holds the token
// signs with it, as the token's own DID.
func DeriveKey(from ed25519.PrivateKey, purpose string) (ed25519.PrivateKey, error) {
	if len(from) != ed25519.PrivateKeySize {
		return nil, errors.Newf("a token is derived from an ed25519 key, and this one is %d bytes", len(from))
	}
	if purpose == "" {
		return nil, errors.New("a token is derived for a purpose, and this named none")
	}
	// The HMAC of the purpose under the key's own seed: a DID of its own, and
	// nothing given back of the key it came from.
	mac := hmac.New(sha256.New, from.Seed())
	if _, err := mac.Write([]byte("qntx:derived-token:" + purpose)); err != nil {
		return nil, errors.Wrapf(err, "the seed of the token for %s was not derived", purpose)
	}
	return ed25519.NewKeyFromSeed(mac.Sum(nil)), nil
}
