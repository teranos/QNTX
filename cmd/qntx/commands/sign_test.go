package commands

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
)

// "find a way to sign the request?"
// qntx sign prints what one request to a node goes with, for curl's -H.
func TestSignPrintsWhatOneRequestGoesWith(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := keyIn(t, home)
	did := access.EncodeDIDKey(key.Public().(ed25519.PublicKey))

	var out bytes.Buffer
	SignCmd.SetOut(&out)
	SignCmd.SetArgs([]string{"https://api.q.sbvh.nl"})
	require.NoError(t, SignCmd.Execute())

	line := strings.TrimSpace(out.String())
	signed, isSigned := strings.CutPrefix(line, "Signed ")
	require.True(t, isSigned, "printed no signature: %q", line)
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return key.Public(), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithAudience("https://api.q.sbvh.nl"), jwt.WithExpirationRequired())
	require.NoError(t, err)
	assert.Equal(t, did, claims.Issuer)
}
