package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/QNTX/internal/access"

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
// way cmd/qntx/qntx/main.go enables one, and a bearer token minted at each level.
func pluginServingServer(t *testing.T, name string) (*QNTXServer, map[auth.Level]string) {
	t.Helper()

	logger := zaptest.NewLogger(t).Sugar()
	tokens := &heldTokens{grants: map[string]auth.Grant{}}
	users := testUsers(t)
	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, users, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	registry := plugin.NewRegistry("0.1.0", logger)
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
		// A plugin answering a sigil is handed its caller's store.
		held: servingStub(&stubStore{}),
	}
	srv.setupHTTPRoutes()
	require.NoError(t, srv.open())

	callers := map[auth.Level]string{}
	for _, level := range []auth.Level{auth.LevelRoot, auth.LevelSuper} {
		callers[level] = tokens.caller(t, level, h.PublicOrigin())
	}
	return srv, callers
}

func asBearer(method, path, caller string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", authorizationFor(caller))
	return req
}

// "I WANT TO KILL THIS IDEA, I DONT WANT TO MAINTAIN TOKENS, AGENNTS KEEP REACHING FOR IT AND CREATING MORE TOKENS, THIS ISNT SECURE"
// 「[Opus 5.5] A caller is a person's key, said yes to as a device sign-in says yes: the node」
// 「[Opus 5.5] holds a grant for its DID, and each request it makes is signed once, for the」
// 「[Opus 5.5] node's origin. Nothing is presented that could be presented again.」
type signingCaller struct {
	key    ed25519.PrivateKey
	origin string
}

// callers is every caller a test made, by the name its request helpers are handed.
var callers sync.Map

// authorizationFor is a request's Authorization from caller: its signature, made for
// this one request, or what was named as it is when no caller is named so.
func authorizationFor(caller string) string {
	held, ok := callers.Load(caller)
	if !ok {
		return "Bearer " + caller
	}
	c := held.(signingCaller)
	did := access.EncodeDIDKey(c.key.Public().(ed25519.PublicKey))
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		panic(err)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer: did, Subject: did, Audience: jwt.ClaimStrings{c.origin},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), ID: hex.EncodeToString(jti),
	}).SignedString(c.key)
	if err != nil {
		panic(err)
	}
	return "Signed " + signed
}

// heldTokens is a TokenStore holding the grants of this test's callers and nothing else.
type heldTokens struct {
	grants map[string]auth.Grant
	held   []auth.TokenInfo
}

// rootsClient is ROOT's own client: what ROOT's connector, claude.ai's or ChatGPT's,
// signs in through (ADR-038).
const rootsClient = "did:key:zroots-client"

// caller is who asks at level. ROOT is ROOT's connector: a bearer its sign-in issued
// through ROOT's client, which the gate admits as the person. SUPER is no person's
// rung: it is a key granted SUPER, as auth.grants in am.toml grants one, signing each
// request it makes.
func (h *heldTokens) caller(t *testing.T, level auth.Level, origin string) string {
	t.Helper()
	never := "2999-01-01T00:00:00Z"
	if level == auth.LevelRoot {
		if _, held := h.ByDID(rootsClient); !held {
			h.held = append(h.held, auth.TokenInfo{ID: "AT-roots-client", Label: "ROOT's client", DID: rootsClient,
				MintedBy: rootAccount, Level: auth.LevelOAuth, Root: true, ExpiresAt: &never})
		}
		raw := "qntx_roots_connector_" + t.Name()
		sum := sha256.Sum256([]byte(raw))
		h.grants[hex.EncodeToString(sum[:])] = auth.Grant{MintedBy: rootAccount, Level: auth.LevelRoot,
			DID: "did:key:zroots-connector", ClientDID: rootsClient}
		return raw
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	did := access.EncodeDIDKey(key.Public().(ed25519.PublicKey))
	h.held = append(h.held, auth.TokenInfo{ID: "AT-" + string(level), Label: "caller " + string(level), DID: did,
		MintedBy: rootAccount, Level: level, ExpiresAt: &never})
	name := "caller:" + string(level) + ":" + did
	callers.Store(name, signingCaller{key: key, origin: origin})
	t.Cleanup(func() { callers.Delete(name) })
	return name
}

// ByDID is the caller whose key this is.
func (h *heldTokens) ByDID(did string) (auth.TokenInfo, bool) {
	for _, info := range h.held {
		if info.DID == did {
			return info, true
		}
	}
	return auth.TokenInfo{}, false
}

func (h *heldTokens) Lookup(hash string) (auth.Grant, bool) {
	grant, ok := h.grants[hash]
	return grant, ok
}

// Nothing here is ever revoked or expired, so a token this holds is a token
// that works: held and live are the same fact.
func (h *heldTokens) LookupSpent(hash string) (auth.Grant, bool, bool) {
	grant, ok := h.grants[hash]
	return grant, ok, ok
}

func (h *heldTokens) Create(auth.NewToken) (string, string, error) { return "", "", nil }
func (h *heldTokens) Issue(auth.IssuedToken) (string, error)       { return "", nil }
func (h *heldTokens) List() ([]auth.TokenInfo, error)              { return h.held, nil }
func (h *heldTokens) Revoke(string) error                          { return nil }
func (h *heldTokens) Enable(string) error                          { return nil }
func (h *heldTokens) Touch(string) error                           { return nil }

// fakePlugin answers on everything it is asked, saying so, so the test can
// tell the plugin's answer from one the node wrote on its behalf.
type fakePlugin struct {
	name string
}

func (p *fakePlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Name: p.name, Version: "0.0.0", QNTXVersion: ">= 0.0.0"}
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
