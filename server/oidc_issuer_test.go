package server

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestEachNamespaceHasItsOwnStableOIDCIssuer(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	garden, err := s.namespaceIssuer("garden")
	require.NoError(t, err)
	gardenAgain, err := s.namespaceIssuerForPath("garden")
	require.NoError(t, err)
	orchard, err := s.namespaceIssuer("orchard")
	require.NoError(t, err)
	assert.Equal(t, garden.url, gardenAgain.url)
	assert.Equal(t, garden.jwk.KeyID, gardenAgain.jwk.KeyID)
	assert.NotEqual(t, garden.url, orchard.url)
	assert.NotEqual(t, garden.jwk.KeyID, orchard.jwk.KeyID)

	discovery := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, namespaceOIDCPath+oidcDiscoveryPath, nil)
	request.SetPathValue("namespace", "garden")
	s.HandleNamespaceOIDCDiscovery(discovery, request)
	require.Equal(t, http.StatusOK, discovery.Code)
	var document map[string]any
	require.NoError(t, json.Unmarshal(discovery.Body.Bytes(), &document))
	assert.Equal(t, garden.url, document["issuer"])
	assert.Equal(t, garden.url+oidcKeysPath, document["jwks_uri"])

	keys := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, namespaceOIDCPath+oidcKeysPath, nil)
	request.SetPathValue("namespace", "garden")
	s.HandleNamespaceOIDCKeys(keys, request)
	require.Equal(t, http.StatusOK, keys.Code)
	var published jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(keys.Body.Bytes(), &published))
	require.Len(t, published.Keys, 1)
	assert.Equal(t, garden.jwk.KeyID, published.Keys[0].KeyID)
}

func TestNamespaceIssuerSignsNamespaceScopedIdentityTokens(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	issuer, err := s.namespaceIssuer("garden")
	require.NoError(t, err)
	subject, err := s.namespaceAnthropicSubject("garden")
	require.NoError(t, err)
	token, err := issuer.identityToken(subject, "garden")
	require.NoError(t, err)
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	var claims jwt.Claims
	var custom struct {
		Namespace string `json:"namespace"`
	}
	require.NoError(t, parsed.Claims(issuer.jwk.Key, &claims, &custom))
	assert.Equal(t, issuer.url, claims.Issuer)
	assert.Equal(t, subject, claims.Subject)
	assert.Equal(t, "garden", custom.Namespace)
}

func TestNamespaceOIDCIssuerPathDoesNotNameUnknownNamespaces(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	_, err := s.namespaceIssuerForPath("not-a-namespace")
	require.Error(t, err)

	request := httptest.NewRequest(http.MethodGet, namespaceOIDCPath+oidcKeysPath, nil)
	request.SetPathValue("namespace", "not-a-namespace")
	recorder := httptest.NewRecorder()
	s.HandleNamespaceOIDCKeys(recorder, request)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}
