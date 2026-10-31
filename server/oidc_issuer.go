package server

// The node as an OpenID Connect issuer (OpenID Connect Discovery 1.0): it
// signs an identity token for each of its agents, and Anthropic's workload
// identity federation trades the token for a short-lived access token.
// "we will not be doing API key anymore"

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/access"
	errors "github.com/teranos/sacred-error"
)

const (
	oidcDiscoveryPath = "/.well-known/openid-configuration"
	oidcKeysPath      = "/.well-known/jwks.json"
	// identityTokenAudience is who an identity token is for.
	identityTokenAudience = "https://api.anthropic.com"
	// identityTokenLifetime is within the issuer's maximum Anthropic holds it to.
	identityTokenLifetime = 5 * time.Minute
)

// issuer is the node as an OIDC issuer: where it answers, and the key it signs with.
type issuer struct {
	url string
	key *ecdsa.PrivateKey
	jwk jose.JSONWebKey
}

// theIssuer is the node as an OIDC issuer at its public origin.
func (s *QNTXServer) theIssuer() (*issuer, error) {
	if s.nodeDID == nil {
		return nil, errors.New("this node has no key of its own to derive its issuer key from")
	}
	if s.authHandler == nil {
		return nil, errors.New("this node runs without auth, so it has no public origin to issue identity tokens from")
	}
	return newIssuer(s.authHandler.PublicOrigin(), s.nodeDID.PrivateKey)
}

// newIssuer is the issuer at url whose key is derived from node.
func newIssuer(url string, node ed25519.PrivateKey) (*issuer, error) {
	key, err := access.DeriveIssuerKey(node)
	if err != nil {
		return nil, err
	}
	jwk := jose.JSONWebKey{Key: key.Public(), Algorithm: string(jose.ES256), Use: "sig"}
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, errors.Wrapf(err, "the issuer key of %s has no thumbprint to name it by", url)
	}
	jwk.KeyID = base64.RawURLEncoding.EncodeToString(thumbprint)
	return &issuer{url: url, key: key, jwk: jwk}, nil
}

// identityToken is a fresh identity token for agent: its subject is the
// agent's DID, and namespace the namespace it stands in. Each carries its own
// jti, so each is traded once.
func (i *issuer) identityToken(subject, namespace string) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: i.key, KeyID: i.jwk.KeyID}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", errors.Wrapf(err, "no signer for the identity token of %s", subject)
	}
	now := time.Now()
	claims := jwt.Claims{
		Issuer: i.url, Subject: subject, Audience: jwt.Audience{identityTokenAudience},
		IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now),
		Expiry: jwt.NewNumericDate(now.Add(identityTokenLifetime)), ID: uuid.NewString(),
	}
	signed, err := jwt.Signed(signer).Claims(claims).Claims(map[string]any{"namespace": namespace}).Serialize()
	return signed, errors.Wrapf(err, "the identity token of %s in %s was not signed", subject, namespace)
}

// identityTokenOf is what the SDK calls for each exchange the agent makes.
func (i *issuer) identityTokenOf(subject, namespace string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return i.identityToken(subject, namespace) }
}

// HandleOIDCDiscovery answers where the issuer's keys are.
func (s *QNTXServer) HandleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	i, err := s.theIssuer()
	if err != nil {
		writeError(w, http.StatusNotFound, "this node issues no identity tokens: "+err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                i.url,
		"jwks_uri":                              i.url + oidcKeysPath,
		"id_token_signing_alg_values_supported": []string{string(jose.ES256)},
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
	})
}

// HandleOIDCKeys answers the issuer's public key.
func (s *QNTXServer) HandleOIDCKeys(w http.ResponseWriter, r *http.Request) {
	i, err := s.theIssuer()
	if err != nil {
		writeError(w, http.StatusNotFound, "this node issues no identity tokens: "+err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{i.jwk}})
}
