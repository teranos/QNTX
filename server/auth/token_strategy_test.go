package auth

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ory/fosite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fosite hands out the token Create hands out: 32 bytes, hex, qntx_-prefixed,
// named by the SHA-256 the store keeps (ADR-025).
func TestTokenStrategyIssuesAQNTXToken(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy

	token, signature, err := s.GenerateAccessToken(ctx, nil)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(token, "qntx_"), "token %q lacks the qntx_ prefix", token)
	seed, err := hex.DecodeString(strings.TrimPrefix(token, "qntx_"))
	require.NoError(t, err, "token %q is not hex after the prefix", token)
	assert.Len(t, seed, 32)

	assert.Equal(t, sha256Hex(token), signature, "the signature is the hash the store keeps")
	assert.Equal(t, signature, s.AccessTokenSignature(ctx, token))

	assert.NoError(t, s.ValidateAccessToken(ctx, nil, token))
}

func TestTokenStrategyMintsEachTokenOnce(t *testing.T) {
	ctx := context.Background()
	var s TokenStrategy

	first, _, err := s.GenerateAccessToken(ctx, nil)
	require.NoError(t, err)
	second, _, err := s.GenerateAccessToken(ctx, nil)
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
