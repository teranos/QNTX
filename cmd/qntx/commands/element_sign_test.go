package commands

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
)

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"

// 「[Sonnet 5.5] heardBy is a node that keeps the Authorization each request carried.」
func heardBy(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var heard []string
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heard = append(heard, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(node.Close)
	return node, &heard
}

// 「[Sonnet 5.5] keyIn keeps the zero seed in home's ~/.qntx/key and answers its key.」
func keyIn(t *testing.T, home string) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".qntx"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".qntx", "key"), []byte(hex.EncodeToString(seed)+"\n"), 0o600))
	return ed25519.NewKeyFromSeed(seed)
}

// 「[Sonnet 5.5] Each request is signed for itself by the key in ~/.qntx/key, made out to the node it goes to.」
func TestARequestIsSignedByTheKeyKept(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key := keyIn(t, home)
	did := access.EncodeDIDKey(key.Public().(ed25519.PublicKey))
	node, heard := heardBy(t)

	_, err := ask(http.MethodGet, node.URL+"/api/attestations?subject=x", nil)
	require.NoError(t, err)
	_, err = ask(http.MethodGet, node.URL+"/api/attestations?subject=x", nil)
	require.NoError(t, err)
	require.Len(t, *heard, 2)
	assert.NotEqual(t, (*heard)[0], (*heard)[1], "one signature was sent twice")

	signed, isSigned := strings.CutPrefix((*heard)[0], "Signed ")
	require.True(t, isSigned, "what was sent is no signature: %q", (*heard)[0])
	var claims jwt.RegisteredClaims
	_, err = jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return key.Public(), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithAudience(node.URL), jwt.WithExpirationRequired())
	require.NoError(t, err)
	assert.Equal(t, did, claims.Issuer)
	assert.Equal(t, did, claims.Subject)
}

// 「[Sonnet 5.5] With no key kept there is nothing to sign with, and nothing is sent.」
func TestNoKeyKeptIsSaid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	node, heard := heardBy(t)
	_, err := ask(http.MethodGet, node.URL+"/api/attestations", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".qntx/key")
	assert.Empty(t, *heard)
}

// 「[Sonnet 5.5] At the door the node opened for the agent asking, nothing is presented.」
func TestAtADoorNothingIsPresented(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	atDoor = true
	t.Cleanup(func() { atDoor = false })
	node, heard := heardBy(t)
	_, err := ask(http.MethodGet, node.URL+"/api/attestations", nil)
	require.NoError(t, err)
	require.Len(t, *heard, 1)
	assert.Empty(t, (*heard)[0])
}
