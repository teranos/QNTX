package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/auth"
)

const loomRepo = "https://github.com/teranos/QNTX/tree/main/qntx-plugins/loom"

// checkingServer is a node whose own GitHub answers from a stand-in holding
// teranos/QNTX with qntx-plugins/loom in it; readme is that directory's README,
// or none when empty. It returns the paths GitHub was asked for.
func checkingServer(t *testing.T, readme string) (*QNTXServer, *[]string) {
	t.Helper()
	s := rootKnowingServer(t)
	table, _, err := auth.OpenTokenTable(qntxtest.CreateTestDB(t), nil)
	require.NoError(t, err)
	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		table, nil, false, []string{rootAccount}, nil)
	require.NoError(t, err)
	s.authHandler = h
	keeper, err := h.GitHubKeeper()
	require.NoError(t, err)
	_, err = keeper.KeepGitHub(auth.NamespaceSystem, rootAccount, auth.GitHubSecret{Token: "ghu_node", Source: auth.GitHubSourceOAuth})
	require.NoError(t, err)

	var asked []string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		switch r.URL.EscapedPath() {
		case "/repos/teranos/QNTX":
			_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "teranos/QNTX", "private": false, "default_branch": "main"})
		case "/repos/teranos/QNTX/contents/qntx-plugins/loom":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"type": "file", "name": "main.go", "path": "qntx-plugins/loom/main.go"}})
		case "/repos/teranos/QNTX/readme/qntx-plugins/loom":
			if readme == "" {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "path": "qntx-plugins/loom/README.md",
				"content": base64.StdEncoding.EncodeToString([]byte(readme))})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)
	return s, &asked
}

// Check asks GitHub as the node, and brings back the plugin's README.
func TestCheckFindsThePluginsDirectoryAndItsReadme(t *testing.T) {
	s, asked := checkingServer(t, "# loom\nWeaves.")

	checked, err := s.checkPlugin(context.Background(), loomRepo)
	require.NoError(t, err)
	assert.Equal(t, "loom", checked.Name)
	assert.Equal(t, "teranos/QNTX", checked.Repository)
	assert.Equal(t, "main", checked.Ref)
	assert.Equal(t, "qntx-plugins/loom", checked.Path)
	assert.Equal(t, "# loom\nWeaves.", checked.Readme)
	assert.Equal(t, "qntx-plugins/loom/README.md", checked.ReadmePath)
	assert.Contains(t, *asked, "/repos/teranos/QNTX/contents/qntx-plugins/loom?ref=main", "the path keeps its slashes")
}

// A plugin with no README is still there; what GitHub said is kept.
func TestCheckWithoutAReadmeSaysWhy(t *testing.T) {
	s, _ := checkingServer(t, "")

	checked, err := s.checkPlugin(context.Background(), loomRepo)
	require.NoError(t, err)
	assert.Empty(t, checked.Readme)
	assert.Contains(t, checked.ReadmeSaid, "404")
}

// A tree URL naming a directory the repository does not have is refused.
func TestCheckRefusesAPathTheRepositoryDoesNotHave(t *testing.T) {
	s, _ := checkingServer(t, "")

	_, err := s.checkPlugin(context.Background(), "https://github.com/teranos/QNTX/tree/main/qntx-plugins/nothere")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qntx-plugins/nothere")
}

// With GitHub switched off on the node, Check asks nothing and says so.
func TestCheckWithGitHubOffOnTheNodeIsRefused(t *testing.T) {
	s, asked := checkingServer(t, "")
	require.NoError(t, s.nodeRecords().SetGitHub(rootAccount, GitHubSettings{Enabled: false, RunnerPath: DefaultRunnerPath}))

	_, err := s.checkPlugin(context.Background(), loomRepo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
	assert.Empty(t, *asked)
}

// Only github.com holds a plugin's repository.
func TestCheckRefusesAnotherHost(t *testing.T) {
	s, _ := checkingServer(t, "")
	_, err := s.checkPlugin(context.Background(), "https://codeberg.org/teranos/pyre")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codeberg.org")
}
