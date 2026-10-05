package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// askedOfGitHub is one thing the stand-in for GitHub was asked.
type askedOfGitHub struct {
	Route string
	Body  string
}

// appHoldingServer is a node that holds a GitHub App, whose GitHub is a
// stand-in with the App installed on teranos/QNTX and granting contents.
func appHoldingServer(t *testing.T, contents string) (*QNTXServer, *[]askedOfGitHub) {
	t.Helper()
	s := githubKnowingServer(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s.authHandler.SetGitHubApp("Iv23li-the-app", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))

	asked := &[]askedOfGitHub{}
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*asked = append(*asked, askedOfGitHub{Route: r.Method + " " + r.URL.Path, Body: string(body)})
		// What the App signs is a JWT, and a JWT's header starts ey.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "A JSON web token could not be decoded"})
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/teranos/QNTX/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": "the-app"})
		case "POST /app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_minted", "expires_at": "2026-10-05T10:20:00Z",
				"permissions": map[string]string{"contents": contents, "metadata": "read"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s, asked
}

// What carries an agent's push is a token the node mints as the App, for the
// one repository asked about, when git asks for it (ADR-048).
func TestTheNodeMintsAGitCredentialForOneRepository(t *testing.T) {
	s, asked := appHoldingServer(t, "write")

	got, refused := answer(t, s, "credential", sigil.Sent{"owner": "teranos", "repo": "QNTX"})
	require.Nil(t, refused)
	credential, ok := got.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "x-access-token", credential["username"])
	assert.Equal(t, "ghs_minted", credential["password"])
	assert.Equal(t, "2026-10-05T10:20:00Z", credential["expires_at"])
	assert.Equal(t, "the-app", credential["app"])
	assert.Equal(t, "write", credential["contents"])
	holds(t, s.githubSignum(), "credential", got)

	require.Len(t, *asked, 2)
	assert.Equal(t, "GET /repos/teranos/QNTX/installation", (*asked)[0].Route)
	assert.Equal(t, "POST /app/installations/7/access_tokens", (*asked)[1].Route)
	// Narrowed to the repository asked about, whatever else the installation covers.
	assert.JSONEq(t, `{"repositories": ["QNTX"]}`, (*asked)[1].Body)
}

// An App that only reads contents still mints, and the credential says so: a
// push it carries is refused by GitHub, and this is where the reason is read.
func TestAGitCredentialSaysWhetherItMayWriteContents(t *testing.T) {
	s, _ := appHoldingServer(t, "read")
	got, refused := answer(t, s, "credential", sigil.Sent{"owner": "teranos", "repo": "QNTX"})
	require.Nil(t, refused)
	assert.Equal(t, "read", got.(map[string]any)["contents"])
}

// Where the App is not installed there is no credential, in GitHub's words.
func TestNoGitCredentialWhereTheAppIsNotInstalled(t *testing.T) {
	s, asked := appHoldingServer(t, "write")
	_, refused := answer(t, s, "credential", sigil.Sent{"owner": "elsewhere", "repo": "thing"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "elsewhere/thing")
	assert.Contains(t, refused.GetSays(), "Not Found")
	assert.Len(t, *asked, 1, "a token was asked for where no installation was found")
}

// A node that holds no App's key mints nothing, and says which key is missing.
func TestNoGitCredentialWithoutTheAppsKey(t *testing.T) {
	s := githubKnowingServer(t)
	_, refused := answer(t, s, "credential", sigil.Sent{"owner": "teranos", "repo": "QNTX"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "private_key")
}
