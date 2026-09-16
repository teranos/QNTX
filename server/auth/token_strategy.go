package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

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
	// Who said yes at the door, carried to the mint (ADR-025): the token
	// speaks for them, the way a token minted in the glyph speaks for the
	// session that minted it.
	MintedBy            string `json:"minted_by"`
	MintedByUser        string `json:"minted_by_user"`
	MintedByDisplayName string `json:"minted_by_display_name"`
	// Namespace is the door the client was minted at (ADR-032), which is
	// where the token acts.
	Namespace string `json:"namespace"`
}

// Clone is the whole session rather than the half a promoted method copies.
// fosite clones the session on a refresh (flow_refresh.go), and
// fosite.DefaultSession is embedded by value: the promoted Clone answers a
// *DefaultSession, and the DID, the person and the namespace are gone with it.
// The two maps are copied because a shared one is the same bug one level down.
func (s *TokenSession) Clone() fosite.Session {
	if s == nil {
		return nil
	}
	copied := *s
	copied.ExpiresAt = make(map[fosite.TokenType]time.Time, len(s.ExpiresAt))
	for kind, at := range s.ExpiresAt {
		copied.ExpiresAt[kind] = at
	}
	if s.Extra != nil {
		copied.Extra = make(map[string]any, len(s.Extra))
		for key, value := range s.Extra {
			copied.Extra[key] = value
		}
	}
	return &copied
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

// MintToken draws the raw token and the DID it names: 32 random bytes,
// hex-encoded, `qntx_` prefixed (ADR-025:16). The bytes are an ed25519 seed,
// so the token has a public half worth naming and its holder can sign as it.
//
// The one place a token is drawn, whether the mint glyph asks the store or
// fosite asks the strategy.
func MintToken() (raw, did string, err error) {
	seed := make([]byte, tokenSeedBytes)
	if _, err := rand.Read(seed); err != nil {
		return "", "", errors.Wrap(err, "failed to read a seed for an access token")
	}
	pub, isEd25519 := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !isEd25519 {
		return "", "", errors.Newf(
			"an ed25519 seed produced a %T public half, so the token has no DID to be named by",
			ed25519.NewKeyFromSeed(seed).Public())
	}
	return tokenPrefix + hex.EncodeToString(seed), EncodeDIDKey(pub), nil
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
	token, did, err := MintToken()
	if err != nil {
		return "", "", err
	}
	session.DID = did
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
