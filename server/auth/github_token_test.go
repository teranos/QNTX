package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// A GitHub App's user access token expires and comes with a refresh token.
// The exchange keeps all of it, so the node can spend it later.
func TestGitHubExchangeKeepsTheUserAccessToken(t *testing.T) {
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":             "ghu_spend",
				"expires_in":               28800,
				"refresh_token":            "ghr_refresh",
				"refresh_token_expires_in": 15897600,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1739143, "login": "teranos"})
	})

	before := time.Now().UnixMilli()
	acct, err := githubExchange(context.Background(),
		providerState{ClientID: "client-id", ClientSecret: "client-secret"},
		"the-code", "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	require.NotNil(t, acct.github)
	assert.Equal(t, "ghu_spend", acct.github.Token)
	assert.Equal(t, "ghr_refresh", acct.github.Refresh)
	assert.Equal(t, "client-id", acct.github.Client)
	assert.Equal(t, GitHubSourceOAuth, acct.github.Source)
	assert.Equal(t, "teranos", acct.github.Login)
	require.NotNil(t, acct.github.ExpiresAt)
	assert.GreaterOrEqual(t, *acct.github.ExpiresAt, before+28800*1000)
}

// One GITHUB token per namespace: keeping another replaces it, so the
// namespace spends whoever set it up last.
func TestANamespaceSpendsOneGitHubToken(t *testing.T) {
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)

	_, found, err := table.GitHubIn(NamespaceSystem)
	require.NoError(t, err)
	assert.False(t, found, "a namespace has no GitHub until somebody sets it up")

	first, err := table.KeepGitHub(NamespaceSystem, "github:1", GitHubSecret{Token: "ghu_one", Source: GitHubSourceOAuth})
	require.NoError(t, err)
	second, err := table.KeepGitHub(NamespaceSystem, "github:2", GitHubSecret{Token: "ghu_two", Source: GitHubSourceOAuth})
	require.NoError(t, err)
	assert.Equal(t, first, second, "the namespace's token is replaced, not added to")

	held, found, err := table.GitHubIn(NamespaceSystem)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "ghu_two", held.GitHub.Token)
	assert.Equal(t, "github:2", held.MintedBy)
	assert.Equal(t, string(LevelGitHub), held.Level)

	_, found, err = table.GitHubIn("clean")
	require.NoError(t, err)
	assert.False(t, found, "another namespace does not spend the node's")
}

// What a list answers never carries the GitHub token.
func TestAGitHubTokenIsNeverListed(t *testing.T) {
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	_, err = table.KeepGitHub(NamespaceSystem, "github:1", GitHubSecret{Token: "ghu_secret", Refresh: "ghr_secret", Source: GitHubSourceOAuth})
	require.NoError(t, err)

	listed, err := table.List()
	require.NoError(t, err)
	body, err := json.Marshal(listed)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "ghu_secret")
	assert.NotContains(t, string(body), "ghr_secret")
}

// The node spends a GITHUB token at GitHub. Presented to a route, it is
// nothing.
func TestAGitHubTokenIsNotABearer(t *testing.T) {
	h := &Handler{logger: zap.NewNop().Sugar()}
	_, admitted := h.admissionOf(Presented{Bearer: &Grant{Level: LevelGitHub, MintedBy: "github:1"}})
	assert.False(t, admitted)
}

// A token about to expire is refreshed with the client that issued it before
// it is spent, and the refreshed one is what the namespace keeps.
func TestAnExpiringGitHubTokenIsRefreshedBeforeItIsSpent(t *testing.T) {
	var asked map[string]string
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		asked = map[string]string{}
		for key := range r.PostForm {
			asked[key] = r.PostForm.Get(key)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ghu_new",
			"expires_in":    28800,
			"refresh_token": "ghr_new",
		})
	})

	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	soon := time.Now().Add(10 * time.Second).UnixMilli()
	_, err = table.KeepGitHub(NamespaceSystem, "github:1", GitHubSecret{
		Token: "ghu_old", Refresh: "ghr_old", ExpiresAt: &soon, Client: "client-id", Source: GitHubSourceOAuth,
	})
	require.NoError(t, err)

	h := &Handler{logger: zap.NewNop().Sugar(), tokens: table}
	h.SetGitHubClient("client-id", "client-secret")

	token, key, err := h.GitHubToken(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "ghu_new", token)
	assert.NotEmpty(t, key)
	assert.Equal(t, "refresh_token", asked["grant_type"])
	assert.Equal(t, "ghr_old", asked["refresh_token"])
	assert.Equal(t, "client-id", asked["client_id"])
	assert.Equal(t, "client-secret", asked["client_secret"])

	held, _, err := table.GitHubIn(NamespaceSystem)
	require.NoError(t, err)
	assert.Equal(t, "ghu_new", held.GitHub.Token)
	assert.Equal(t, "ghr_new", held.GitHub.Refresh)
}

// A namespace nobody set up has no GitHub, and saying so is the answer.
func TestANamespaceWithNoGitHubIsRefused(t *testing.T) {
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	h := &Handler{logger: zap.NewNop().Sugar(), tokens: table}

	_, _, err = h.GitHubToken(context.Background(), "clean")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clean")
}

// The App's webhook secret exists once ROOT generates it, and generating again
// replaces it. It is no namespace's GitHub.
func TestROOTGeneratesTheWebhookSecret(t *testing.T) {
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	h := &Handler{logger: zap.NewNop().Sugar(), tokens: table}

	_, found, err := h.GitHubWebhook()
	require.NoError(t, err)
	assert.False(t, found, "no secret until ROOT generates one")

	first, err := h.NewGitHubWebhook("github:1")
	require.NoError(t, err)
	second, err := h.NewGitHubWebhook("github:1")
	require.NoError(t, err)
	assert.NotEqual(t, first, second)

	held, found, err := h.GitHubWebhook()
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, second, held, "generating again replaces it")

	tokens, err := table.GitHubTokens()
	require.NoError(t, err)
	assert.Empty(t, tokens)

	listed, err := table.List()
	require.NoError(t, err)
	body, err := json.Marshal(listed)
	require.NoError(t, err)
	assert.NotContains(t, string(body), second)
}

// ROOT logging in with GitHub is what gives the node its GitHub. Anybody else
// linking a GitHub account gives the node nothing.
func TestTheNodesGitHubIsROOTs(t *testing.T) {
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	h := &Handler{logger: zap.NewNop().Sugar(), tokens: table}
	h.identities.set([]string{"github:1739143"}, nil)

	h.keepNodeGitHub("github", account{CanonicalID: "github:42", github: &GitHubSecret{Token: "ghu_stranger", Source: GitHubSourceOAuth}})
	_, found, err := table.GitHubIn(NamespaceSystem)
	require.NoError(t, err)
	assert.False(t, found, "a stranger's GitHub is not the node's")

	h.keepNodeGitHub("github", account{CanonicalID: "github:1739143", github: &GitHubSecret{Token: "ghu_root", Source: GitHubSourceOAuth}})
	held, found, err := table.GitHubIn(NamespaceSystem)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "ghu_root", held.GitHub.Token)
	assert.Equal(t, "github:1739143", held.MintedBy)
}
