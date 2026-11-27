package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teranos/QNTX/internal/nodedid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "why cant it take from k, on the same machine?"
// 「[Opus 5.5] A subsystem reads a repository's history from git on its own machine, and」
// 「[Opus 5.5] asks the node only for what git needs to fetch it: a credential for one repository.」

// 「[Opus 5.5] The node holding the App mints the credential itself.」
func TestANodeHoldingTheAppGivesAGitCredentialItself(t *testing.T) {
	s, asked := appHoldingServer(t, "read")

	credential, err := linkedHost{s}.GitCredential(context.Background(), "teranos", "QNTX")
	require.NoError(t, err)
	assert.Equal(t, "x-access-token", credential.Username)
	assert.Equal(t, "ghs_minted", credential.Password)
	require.NotEmpty(t, *asked)
	assert.Equal(t, "POST /app/installations/7/access_tokens", (*asked)[len(*asked)-1].Route)
}

// 「[Opus 5.5] A node that asks GitHub through a relay asks the relay for the credential, as」
// 「[Opus 5.5] itself, for the repository named and no other.」
func TestARelayedNodeAsksItsRelayForAGitCredential(t *testing.T) {
	var authorization, path string
	var named map[string]string
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization, path = r.Header.Get("Authorization"), r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&named))
		_ = json.NewEncoder(w).Encode(map[string]any{"username": "x-access-token", "password": "ghs_relayed", "contents": "read"})
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	credential, err := linkedHost{s}.GitCredential(context.Background(), "teranos", "QNTX")
	require.NoError(t, err)
	assert.Equal(t, "x-access-token", credential.Username)
	assert.Equal(t, "ghs_relayed", credential.Password)
	assert.Equal(t, githubCredentialPath, path)
	assert.Equal(t, map[string]string{"owner": "teranos", "repo": "QNTX"}, named)
	assert.True(t, strings.HasPrefix(authorization, "Signed "), "the credential was asked for with %q", authorization)
}

// 「[Opus 5.5] What the relay refuses is the asker's error, in the relay's words.」
func TestARelaysRefusalOfAGitCredentialIsTheAskersError(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"the App is not installed on o/r"}`))
	}))
	defer q.Close()
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL}))

	_, err = linkedHost{s}.GitCredential(context.Background(), "o", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.Contains(t, err.Error(), "the App is not installed on o/r")
}

// 「[Opus 5.5] A relay over plain HTTP elsewhere is handed nothing signed for a credential either.」
func TestAGitCredentialIsNotAskedOfARelayOverPlainHTTPElsewhere(t *testing.T) {
	s := githubKnowingServer(t)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	s.nodeDID = &nodedid.Handler{PrivateKey: key}
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: "http://q.example.com"}))

	_, err = linkedHost{s}.GitCredential(context.Background(), "o", "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not HTTPS")
}
