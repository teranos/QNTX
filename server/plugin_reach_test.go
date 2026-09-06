package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// A plugin's three paths are served to ROOT with no line in the table, and to
// nobody else. The plugin is a fact of am.toml, not of the table.
func TestAPluginsFaceIsRootsWithoutALine(t *testing.T) {
	srv, tokens := pluginServingServer(t, "fake")

	for _, path := range []string{"/api/fake", "/api/fake/{path...}", "/ws/fake"} {
		assert.Contains(t, srv.Unspoken(), path, path+" was not reported as ROOT's alone")
	}

	for _, path := range []string{"/api/fake", "/api/fake/x", "/ws/fake"} {

		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, asBearer(http.MethodGet, path, tokens[auth.LevelRoot]))
		assert.Equal(t, http.StatusOK, w.Code, path+" for ROOT: "+w.Body.String())
		assert.Contains(t, w.Body.String(), "fake answered", path+" for ROOT was answered by the node, not the plugin")

		w = httptest.NewRecorder()
		srv.served.ServeHTTP(w, asBearer(http.MethodGet, path, tokens[auth.LevelSuper]))
		assert.Equal(t, http.StatusForbidden, w.Code, path+" for SUPER: "+w.Body.String())
	}
}

// pluginServingServer is servedForTest with auth on, one plugin enabled the
// way cmd/qntx/main.go enables one, and a bearer token minted at each level.
func pluginServingServer(t *testing.T, name string) (*QNTXServer, map[auth.Level]string) {
	t.Helper()

	logger := zaptest.NewLogger(t).Sugar()
	tokens := &heldTokens{grants: map[string]auth.Grant{}}
	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, nil, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	registry := plugin.NewRegistry("test-version", logger)
	registry.PreRegister(name)
	require.NoError(t, registry.Register(&fakePlugin{name: name}))
	registry.MarkReady(name)

	srv := &QNTXServer{
		pluginRegistry: registry,
		authHandler:    h,
		authEnabled:    true,
		logger:         logger,
		rlAuth:         newRateLimitGroup(100, 100),
		rlWS:           newRateLimitGroup(100, 100),
		rlWrite:        newRateLimitGroup(100, 100),
		rlRead:         newRateLimitGroup(100, 100),
		rlPublic:       newRateLimitGroup(100, 100),
	}
	srv.setupHTTPRoutes()
	require.NoError(t, srv.open())

	raw := map[auth.Level]string{}
	for _, level := range []auth.Level{auth.LevelRoot, auth.LevelSuper} {
		raw[level] = tokens.mint(level)
	}
	return srv, raw
}

func asBearer(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// heldTokens is a TokenStore holding what this test minted and nothing else.
type heldTokens struct {
	grants map[string]auth.Grant
}

func (h *heldTokens) mint(level auth.Level) string {
	raw := "qntx_test_" + string(level)
	sum := sha256.Sum256([]byte(raw))
	h.grants[hex.EncodeToString(sum[:])] = auth.Grant{MintedBy: rootAccount, Level: level}
	return raw
}

func (h *heldTokens) Lookup(hash string) (auth.Grant, bool) {
	grant, ok := h.grants[hash]
	return grant, ok
}

func (h *heldTokens) Create(auth.NewToken) (string, string, error) { return "", "", nil }
func (h *heldTokens) List() ([]auth.TokenInfo, error)              { return nil, nil }
func (h *heldTokens) Revoke(string) error                          { return nil }
func (h *heldTokens) Enable(string) error                          { return nil }
func (h *heldTokens) SetScope(string, []string, []string) error    { return nil }

// fakePlugin answers on everything it is asked, saying so, so the test can
// tell the plugin's answer from one the node wrote on its behalf.
type fakePlugin struct {
	name string
}

func (p *fakePlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Name: p.name, Version: "0.0.0"}
}

func (p *fakePlugin) Initialize(context.Context, plugin.ServiceRegistry) error { return nil }
func (p *fakePlugin) Shutdown(context.Context) error                           { return nil }

func (p *fakePlugin) RegisterHTTP(mux *http.ServeMux) error {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fake answered /api/" + p.name + r.URL.Path))
	})
	return nil
}

func (p *fakePlugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return map[string]plugin.WebSocketHandler{"/ws/" + p.name: p}, nil
}

func (p *fakePlugin) ServeWS(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("fake answered " + r.URL.Path))
}

func (p *fakePlugin) Health(context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{Healthy: true}
}
