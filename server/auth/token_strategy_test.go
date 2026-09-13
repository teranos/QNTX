package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ory/fosite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A request carrying the session a token is issued on.
func tokenRequest() *fosite.Request {
	return &fosite.Request{Session: &TokenSession{}}
}

// fosite hands out the token Create hands out: 32 bytes, hex, qntx_-prefixed,
// named by the SHA-256 the store keeps (ADR-025).
func TestTokenStrategyIssuesAQNTXToken(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy

	token, signature, err := s.GenerateAccessToken(ctx, tokenRequest())
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(token, "qntx_"), "token %q lacks the qntx_ prefix", token)
	seed, err := hex.DecodeString(strings.TrimPrefix(token, "qntx_"))
	require.NoError(t, err, "token %q is not hex after the prefix", token)
	assert.Len(t, seed, 32)

	assert.Equal(t, sha256Hex(token), signature, "the signature is the hash the store keeps")
	assert.Equal(t, signature, s.AccessTokenSignature(ctx, token))

	assert.NoError(t, s.ValidateAccessToken(ctx, nil, token))
}

// The DID is derived from the raw bytes, and fosite's storage never sees the
// raw. So the strategy computes it and the session carries it.
func TestTokenStrategyCarriesTheDIDOnTheSession(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy
	req := tokenRequest()

	token, _, err := s.GenerateAccessToken(ctx, req)
	require.NoError(t, err)

	session, ok := req.GetSession().(*TokenSession)
	require.True(t, ok)
	require.NotEmpty(t, session.DID)

	// The same derivation the store's Create uses: the seed is the private
	// half, and the DID names its public half.
	seed, err := hex.DecodeString(strings.TrimPrefix(token, "qntx_"))
	require.NoError(t, err)
	pub, ok := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	require.True(t, ok)
	assert.Equal(t, EncodeDIDKey(pub), session.DID)
}

// A token nobody can name is not issued.
func TestTokenStrategyRefusesARequestThatCannotCarryTheDID(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy

	_, _, err := s.GenerateAccessToken(ctx, nil)
	require.Error(t, err)

	_, _, err = s.GenerateAccessToken(ctx, &fosite.Request{Session: &fosite.DefaultSession{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "*fosite.DefaultSession")
}

func TestTokenStrategyMintsEachTokenOnce(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy

	first, _, err := s.GenerateAccessToken(ctx, tokenRequest())
	require.NoError(t, err)
	second, _, err := s.GenerateAccessToken(ctx, tokenRequest())
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

func TestTokenStrategyRefusesTheWrongShape(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy
	sixtyFourHex := strings.Repeat("ab", 32)

	for name, token := range map[string]string{
		"no prefix":    sixtyFourHex,
		"other prefix": "sk_" + sixtyFourHex,
		"too short":    "qntx_" + sixtyFourHex[:62],
		"too long":     "qntx_" + sixtyFourHex + "ab",
		"not hex":      "qntx_" + strings.Repeat("zz", 32),
		"empty":        "",
	} {
		t.Run(name, func(t *testing.T) {
			err := s.ValidateAccessToken(ctx, nil, token)
			require.Error(t, err)
			assert.ErrorIs(t, err, fosite.ErrInvalidTokenFormat)
		})
	}
}
