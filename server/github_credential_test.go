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
	"github.com/teranos/QNTX/server/reach"
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
		presented := r.Header.Get("Authorization")
		refuse := func(status int, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
		}
		// What is the App's own is taken with what the App signs: a JWT, whose
		// header starts ey. Anything else is taken with the token it minted.
		ownedByTheApp := strings.HasSuffix(r.URL.Path, "/installation") || strings.HasPrefix(r.URL.Path, "/app/")
		switch {
		case ownedByTheApp && !strings.HasPrefix(presented, "Bearer ey"):
			refuse(http.StatusUnauthorized, "A JSON web token could not be decoded")
		case !ownedByTheApp && presented != "Bearer ghs_minted":
			refuse(http.StatusUnauthorized, "Bad credentials")
		case r.Method+" "+r.URL.Path == "GET /repos/teranos/QNTX/installation":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "app_slug": "the-app"})
		case r.Method+" "+r.URL.Path == "POST /app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_minted", "expires_at": "2099-01-01T00:00:00Z",
				"permissions": map[string]string{"contents": contents, "metadata": "read"},
			})
		default:
			refuse(http.StatusNotFound, "Not Found")
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

	status, credential := gitAsks(t, s, `{"owner": "teranos", "repo": "QNTX"}`)
	require.Equal(t, http.StatusOK, status, "the node said %v", credential)
	assert.Equal(t, "x-access-token", credential["username"])
	assert.Equal(t, "ghs_minted", credential["password"])
	assert.Equal(t, "2099-01-01T00:00:00Z", credential["expires_at"])
	assert.Equal(t, "the-app", credential["app"])
	assert.Equal(t, "write", credential["contents"])

	require.Len(t, *asked, 2)
	assert.Equal(t, "GET /repos/teranos/QNTX/installation", (*asked)[0].Route)
	assert.Equal(t, "POST /app/installations/7/access_tokens", (*asked)[1].Route)
	// Narrowed to the repository asked about, whatever else the installation covers.
	assert.JSONEq(t, `{"repositories": ["QNTX"]}`, (*asked)[1].Body)
}

// gitAsks is what git's credential helper asks the node, and what it answers.
func gitAsks(t *testing.T, s *QNTXServer, body string) (int, map[string]any) {
	t.Helper()
	answered := httptest.NewRecorder()
	s.HandleGitHubCredential(answered, httptest.NewRequest(http.MethodPost, githubCredentialPath, strings.NewReader(body)))
	var said map[string]any
	require.NoError(t, json.Unmarshal(answered.Body.Bytes(), &said), "the node answered %s", answered.Body.String())
	return answered.Code, said
}

// On the box the agent took the credential as a tool, and the token went into
// its context and from there into a command its transcript kept. It is handed
// to git and offered to no model: it is no sigil, and its route is no tool.
func TestTheGitCredentialIsNoTool(t *testing.T) {
	s, _ := appHoldingServer(t, "write")
	for _, held := range s.githubSignum().GetSigils() {
		assert.NotEqual(t, githubCredentialPath, held.GetHttp().GetPath(), "%s answers where the credential is minted", held.GetName())
	}
	assert.False(t, routeTool(reach.Route{Path: githubCredentialPath}))
	assert.True(t, routeTool(reach.Route{Path: "/api/dev"}), "no route is a tool any more")
}

// An App that only reads contents still mints, and the credential says so: a
// push it carries is refused by GitHub, and this is where the reason is read.
func TestAGitCredentialSaysWhetherItMayWriteContents(t *testing.T) {
	s, _ := appHoldingServer(t, "read")
	status, credential := gitAsks(t, s, `{"owner": "teranos", "repo": "QNTX"}`)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "read", credential["contents"])
}

// Where the App is not installed there is no credential, in GitHub's words.
func TestNoGitCredentialWhereTheAppIsNotInstalled(t *testing.T) {
	s, asked := appHoldingServer(t, "write")
	status, said := gitAsks(t, s, `{"owner": "elsewhere", "repo": "thing"}`)
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, said["error"], "elsewhere/thing")
	assert.Contains(t, said["error"], "Not Found")
	assert.Len(t, *asked, 1, "a token was asked for where no installation was found")
}

// A node that holds no App's key mints nothing, and says which key is missing.
func TestNoGitCredentialWithoutTheAppsKey(t *testing.T) {
	s := githubKnowingServer(t)
	status, said := gitAsks(t, s, `{"owner": "teranos", "repo": "QNTX"}`)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, said["error"], "private_key")
}

// What names no repository is asked nothing of GitHub.
func TestNoGitCredentialForNoRepository(t *testing.T) {
	s, asked := appHoldingServer(t, "write")
	status, said := gitAsks(t, s, `{"owner": "teranos"}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, said["error"], "one repository")
	assert.Empty(t, *asked)
}
