package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/ory/fosite"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/teranos/errors"
)

// "ory/fosite is what we're going to use"

// The flow in Go with fosite, its storage where tokens are stored now
// (ADR-025). What the flow hands out is the token QNTX already hands out.

// tokenPrefix marks a raw token as this node's (ADR-025:16).
const tokenPrefix = "qntx_"

// tokenSeedBytes is the length of the random half: 32 bytes, an ed25519 seed,
// so the token has a public half worth naming.
const tokenSeedBytes = 32

// TokenStrategy is fosite's access token strategy issuing a QNTX token: 32
// random bytes, hex-encoded, `qntx_`-prefixed, the same form the mint glyph
// gets from Create. Its signature is the SHA-256 the store keeps, so a token
// fosite issued and a token a session minted are found by the same lookup.
type TokenStrategy struct{}

var _ fositeoauth2.AccessTokenStrategy = TokenStrategy{}

// AccessTokenSignature is the form of a token that is ever stored.
func (TokenStrategy) AccessTokenSignature(_ context.Context, token string) string {
	return sha256Hex(token)
}

// GenerateAccessToken mints the raw token and names it by its hash. The
// requester is not read: what a token carries beyond its hash (ADR-025) is
// written by the store, not decided here.
func (TokenStrategy) GenerateAccessToken(_ context.Context, _ fosite.Requester) (string, string, error) {
	seed := make([]byte, tokenSeedBytes)
	if _, err := rand.Read(seed); err != nil {
		return "", "", errors.Wrap(err, "failed to read a seed for an access token")
	}
	token := tokenPrefix + hex.EncodeToString(seed)
	return token, sha256Hex(token), nil
}

// ValidateAccessToken checks the form and nothing more. Whether the token is
// live is the store's answer, asked by signature, and fosite asks the store
// before it asks this.
func (TokenStrategy) ValidateAccessToken(_ context.Context, _ fosite.Requester, token string) error {
	if !strings.HasPrefix(token, tokenPrefix) {
		return errors.WithStack(fosite.ErrInvalidTokenFormat.WithHintf("a token starts with %s", tokenPrefix))
	}
	body := strings.TrimPrefix(token, tokenPrefix)
	if len(body) != tokenSeedBytes*2 {
		return errors.WithStack(fosite.ErrInvalidTokenFormat.WithHintf(
			"a token is %d hex characters after %s, and this one is %d", tokenSeedBytes*2, tokenPrefix, len(body)))
	}
	if _, err := hex.DecodeString(body); err != nil {
		return errors.WithStack(fosite.ErrInvalidTokenFormat.WithHint("a token is hex after " + tokenPrefix))
	}
	return nil
}
