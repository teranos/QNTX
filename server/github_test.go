package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// githubKnowingServer is a node with a token store, whose GitHub answers from
// a stand-in that accepts one token.
func githubKnowingServer(t *testing.T) *QNTXServer {
	t.Helper()
	s := rootKnowingServer(t)
	table, _, err := auth.OpenTokenTable(qntxtest.CreateTestDB(t), nil)
	require.NoError(t, err)
	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		table, testUsers(t), false, []string{rootAccount}, nil)
	require.NoError(t, err)
	s.authHandler = h

	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghu_good" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Bad credentials"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"resources": map[string]any{
			"core": map[string]any{"limit": 5000, "remaining": 4999, "used": 1, "reset": 1700000000},
		}})
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s
}

func answer(t *testing.T, s *QNTXServer, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	return s.githubSignum().Answers[name](asRoot(), sent)
}

// "In the GitHub element, I can disable GitHub on the Node entirely."
func TestGitHubCanBeDisabledOnTheNodeEntirely(t *testing.T) {
	s := githubKnowingServer(t)
	keeper, err := s.authHandler.GitHubKeeper()
	require.NoError(t, err)
	_, err = keeper.KeepGitHub(auth.NamespaceSystem, rootAccount, auth.GitHubSecret{Token: "ghu_good", Source: auth.GitHubSourceOAuth})
	require.NoError(t, err)

	_, refused := answer(t, s, "node", sigil.Sent{"enabled": "false"})
	require.Nil(t, refused)

	said, err := s.gitHubService().RateLimit(context.Background(), &protocol.GitHubRateLimitRequest{})
	require.NoError(t, err)
	assert.False(t, said.Success)
	assert.Contains(t, said.Error, "disabled")
}

// "I see which namespaces have it enabled, whether their auth is correct, and
// whether it comes from an access token or OAuth."
func TestTheGitHubElementSeesEveryNamespacesGitHub(t *testing.T) {
	s := githubKnowingServer(t)
	keeper, err := s.authHandler.GitHubKeeper()
	require.NoError(t, err)
	_, err = keeper.KeepGitHub(auth.NamespaceSystem, rootAccount, auth.GitHubSecret{Token: "ghu_good", Source: auth.GitHubSourceOAuth, Login: "teranos"})
	require.NoError(t, err)
	_, err = keeper.KeepGitHub("garden", rootAccount, auth.GitHubSecret{Token: "ghu_stale", Source: auth.GitHubSourceAccessToken})
	require.NoError(t, err)

	said, refused := answer(t, s, "status", sigil.Sent{})
	require.Nil(t, refused)
	status := said.(*protocol.GitHubStatus)
	require.Len(t, status.Namespaces, 2)

	byName := map[string]*protocol.GitHubNamespace{}
	for _, ns := range status.Namespaces {
		byName[ns.Namespace] = ns
	}
	assert.True(t, byName["system"].AuthOk)
	assert.Equal(t, auth.GitHubSourceOAuth, byName["system"].Source)
	assert.Equal(t, "teranos", byName["system"].Login)
	require.NotNil(t, byName["system"].Rate)
	assert.Equal(t, uint32(4999), byName["system"].Rate.Remaining)

	assert.False(t, byName["garden"].AuthOk)
	assert.Contains(t, byName["garden"].AuthError, "Bad credentials")
	assert.Equal(t, auth.GitHubSourceAccessToken, byName["garden"].Source)
}

// "it errors when there is no runner"
func TestEnablingTheRunnerWhereThereIsNoneErrors(t *testing.T) {
	s := githubKnowingServer(t)
	_, refused := answer(t, s, "runner", sigil.Sent{"path": t.TempDir(), "enabled": "true"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.Says, ".runner")

	settings, err := s.nodeRecords().GitHub()
	require.NoError(t, err)
	assert.False(t, settings.RunnerEnabled, "a runner that is not there is not switched on")
}

// "When enabled, runner stats show there."
func TestAnEnabledRunnerShowsItsStats(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := githubKnowingServer(t)
	runner := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(runner, ".runner"), []byte(`{"agentName":"q-api-box"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(runner, "_work"), 0o755))

	_, refused := answer(t, s, "runner", sigil.Sent{"path": runner, "enabled": "true"})
	require.Nil(t, refused)
	t.Cleanup(s.stopRunner)

	said, refused := answer(t, s, "status", sigil.Sent{})
	require.Nil(t, refused)
	status := said.(*protocol.GitHubStatus)
	assert.True(t, status.Runner.Enabled)
	assert.Equal(t, runner, status.Runner.Path)
	require.NotNil(t, status.Runner.Stats)
	assert.Equal(t, "q-api-box", status.Runner.Stats.Name)
}

// The runner path is prefilled.
func TestTheRunnerPathIsPrefilled(t *testing.T) {
	s := githubKnowingServer(t)
	said, refused := answer(t, s, "status", sigil.Sent{})
	require.Nil(t, refused)
	assert.Equal(t, "/opt/actions-runner", said.(*protocol.GitHubStatus).Runner.Path)
}
