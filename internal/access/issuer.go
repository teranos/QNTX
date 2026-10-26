package access

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	errors "github.com/teranos/sacred-error"
)

// DeriveIssuerKey is the P-256 key the node signs its OIDC identity tokens
// with: the same key every time, derived from the node's own. Anthropic's
// workload identity federation accepts no ed25519 signature.
func DeriveIssuerKey(from ed25519.PrivateKey) (*ecdsa.PrivateKey, error) {
	if len(from) != ed25519.PrivateKeySize {
		return nil, errors.Newf("the issuer key is derived from an ed25519 key, and this one is %d bytes", len(from))
	}
	// A scalar past the curve's order is one in 2^32; the next counter is tried.
	for counter := range 8 {
		mac := hmac.New(sha256.New, from.Seed())
		fmt.Fprintf(mac, "qntx:oidc-issuer:ES256:%d", counter)
		key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), mac.Sum(nil))
		if err == nil {
			return key, nil
		}
	}
	return nil, errors.New("no P-256 scalar was derived for the issuer key in 8 counters")
}
