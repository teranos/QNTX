package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "why cant we make q relay it"
func TestARelayAsksTheNodeThatHoldsTheApp(t *testing.T) {
	var asked map[string]any
	var bearer string
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, githubPath+"/ask", r.URL.Path)
		bearer = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&asked))
		_ = json.NewEncoder(w).Encode(map[string]any{"answer": map[string]any{"html_url": "https://github.com/o/r/pull/9"}})
	}))
	defer q.Close()
	t.Setenv("RELAY_TOKEN_FOR_TEST", "qntx_the_token")
	s := githubKnowingServer(t)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL, TokenRef: "env:RELAY_TOKEN_FOR_TEST"}))

	answered, err := linkedHost{s}.AskGitHub(context.Background(), "CreateAPullRequest", map[string]any{"owner": "o", "repo": "r"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"html_url":"https://github.com/o/r/pull/9"}`, string(answered))
	assert.Equal(t, "Bearer qntx_the_token", bearer)
	assert.Equal(t, "CreateAPullRequest", asked["operation"])
	assert.JSONEq(t, `{"owner":"o","repo":"r"}`, asked["request"].(string))
}

func TestARelaysRefusalIsTheAskersError(t *testing.T) {
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"not ROOT"}`))
	}))
	defer q.Close()
	t.Setenv("RELAY_TOKEN_FOR_TEST", "qntx_the_token")
	s := githubKnowingServer(t)
	require.NoError(t, s.nodeRecords().SetRelay(rootAccount, GitHubRelay{URL: q.URL, TokenRef: "env:RELAY_TOKEN_FOR_TEST"}))

	_, err := linkedHost{s}.AskGitHub(context.Background(), "GetAPullRequest", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "not ROOT")
}

// The line keeps where the token is, never the token: a line is read by whoever reads the node's records.
func TestARelayLineHoldsAReferenceAndNotAToken(t *testing.T) {
	assert.NoError(t, relayTokenRef("env:RELAY_TOKEN"))
	assert.NoError(t, relayTokenRef("ssm:///k/box/relay-token"))
	assert.Error(t, relayTokenRef("qntx_the_token_itself"))
	assert.Error(t, relayTokenRef(""))
}

func TestANodeWithNoRelayHasNone(t *testing.T) {
	s := githubKnowingServer(t)
	_, found, err := s.nodeRecords().Relay()
	require.NoError(t, err)
	assert.False(t, found)
}
