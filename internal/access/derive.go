package access

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"

	errors "github.com/teranos/sacred-error"
)

// DeriveKey is the key a node derives for one purpose: the same key every
// time, which what holds it signs with as its own DID. The purpose is written
// down once: agent:root.
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
