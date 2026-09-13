package auth

import (
	"context"
	"crypto/ed25519"
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
const tokenSeedBytes = ed25519.SeedSize

// TokenSession is what a fosite request carries about the token being issued.
//
// fosite hands its storage the signature and never the raw token, and the
// DID is derived from the raw bytes. So the DID is computed where the raw
// exists, in the strategy, and carried here to the store.
type TokenSession struct {
	fosite.DefaultSession
	// DID is the did:key the token's seed names. Set by GenerateAccessToken.
	DID string `json:"did"`
}

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

// GenerateAccessToken mints the raw token, names it by its hash, and puts
// the DID its seed names on the request's TokenSession. A request whose
// session cannot carry the DID is refused rather than issued a token nobody
// can name: a dropped field is an error.
func (TokenStrategy) GenerateAccessToken(_ context.Context, requester fosite.Requester) (string, string, error) {
	session, err := tokenSessionOf(requester)
	if err != nil {
		return "", "", err
	}
	seed := make([]byte, tokenSeedBytes)
	if _, err := rand.Read(seed); err != nil {
		return "", "", errors.Wrap(err, "failed to read a seed for an access token")
	}
	pub, isEd25519 := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !isEd25519 {
		return "", "", errors.New("an ed25519 seed produced no ed25519 public half, so the token has no DID to be named by")
	}
	session.DID = EncodeDIDKey(pub)
	token := tokenPrefix + hex.EncodeToString(seed)
	return token, sha256Hex(token), nil
}

// tokenSessionOf is the TokenSession a request carries, or why it carries none.
func tokenSessionOf(requester fosite.Requester) (*TokenSession, error) {
	if requester == nil {
		return nil, errors.New("no request to issue a token for")
	}
	session, ok := requester.GetSession().(*TokenSession)
	if !ok {
		return nil, errors.Newf("the request's session is %T and cannot carry the token's DID; a token is issued on a *TokenSession", requester.GetSession())
	}
	return session, nil
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
