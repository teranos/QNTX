package server

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An identity token verifies against the key the issuer publishes, and says
// what Anthropic's federation checks: iss, sub, aud, iat, exp, a kid, a jti.
func TestIdentityTokenVerifiesAgainstThePublishedKey(t *testing.T) {
	_, node, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	i, err := newIssuer("https://api.q.sbvh.nl", node)
	require.NoError(t, err)
	again, err := newIssuer("https://api.q.sbvh.nl", node)
	require.NoError(t, err)
	assert.Equal(t, i.jwk.KeyID, again.jwk.KeyID, "the issuer key is the same every time it is derived")

	signed, err := i.identityToken("did:key:z6Mkagent", "sbvh")
	require.NoError(t, err)
	parsed, err := jwt.ParseSigned(signed, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	require.Len(t, parsed.Headers, 1)
	assert.Equal(t, i.jwk.KeyID, parsed.Headers[0].KeyID)

	var claims jwt.Claims
	var ours struct {
		Namespace string `json:"namespace"`
	}
	published := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{i.jwk}}
	require.NoError(t, parsed.Claims(published.Key(i.jwk.KeyID)[0].Key, &claims, &ours))
	require.NoError(t, claims.ValidateWithLeeway(jwt.Expected{
		Issuer: "https://api.q.sbvh.nl", Subject: "did:key:z6Mkagent",
		AnyAudience: jwt.Audience{identityTokenAudience}, Time: time.Now(),
	}, 0))
	assert.Equal(t, "sbvh", ours.Namespace)
	assert.NotEmpty(t, claims.ID)
	assert.LessOrEqual(t, claims.Expiry.Time().Sub(claims.IssuedAt.Time()), time.Hour, "within Anthropic's default maximum lifetime")

	other, err := i.identityToken("did:key:z6Mkagent", "sbvh")
	require.NoError(t, err)
	reparsed, err := jwt.ParseSigned(other, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	var second jwt.Claims
	require.NoError(t, reparsed.Claims(i.jwk.Key, &second))
	assert.NotEqual(t, claims.ID, second.ID, "each token is traded once")
}
