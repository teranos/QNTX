package server

import (
	"crypto/ed25519"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap/zaptest"
)

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"

// 「[Opus 5.5] doorOfTheNodeAgent is a node not serving yet, and its Node Agent's door: a」
// 「[Opus 5.5] request the door lets through is told the node does not serve yet.」
func doorOfTheNodeAgent(t *testing.T) (*QNTXServer, string) {
	t.Helper()
	_, db := createTestStore(t)
	tokens, _, err := auth.OpenTokenTable(db, nil)
	require.NoError(t, err)
	h, err := auth.New(db, "localhost", nil, 8770, 8820, 24, zaptest.NewLogger(t).Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, testUsers(t), false, []string{rootAccount}, nil)
	require.NoError(t, err)
	node := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), authHandler: h, nodeDID: &nodedid.Handler{PrivateKey: node}, logger: zaptest.NewLogger(t).Sugar()}
	require.NoError(t, s.nameNodeAgent(t.TempDir()))
	require.True(t, strings.HasPrefix(s.nodeAgent.door, "http://127.0.0.1:"))
	return s, s.nodeAgent.door
}

func knock(t *testing.T, door string, dress func(*http.Request)) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, door+"/mcp", nil)
	require.NoError(t, err)
	dress(req)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

// 「[Opus 5.5] Its agent, which names no origin, is let through.」
func TestAnAgentIsLetThroughItsDoor(t *testing.T) {
	_, door := doorOfTheNodeAgent(t)
	assert.Equal(t, http.StatusServiceUnavailable, knock(t, door, func(*http.Request) {}), "the agent was not let through to the node")
}

// 「[Opus 5.5] A page in a browser names where it came from, and is not let through.」
func TestAPageIsNotLetThroughAnAgentsDoor(t *testing.T) {
	_, door := doorOfTheNodeAgent(t)
	assert.Equal(t, http.StatusForbidden, knock(t, door, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }))
}

// 「[Opus 5.5] A name made to resolve to loopback is not the door's own, and is not let through.」
func TestANameReboundToLoopbackIsNotLetThrough(t *testing.T) {
	_, door := doorOfTheNodeAgent(t)
	assert.Equal(t, http.StatusForbidden, knock(t, door, func(r *http.Request) { r.Host = "evil.example" }))
}
