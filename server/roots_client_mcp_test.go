package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// "but i want a MCP specifically for the Node, only for ROOT"
//
// End to end through the node's own gate: a token issued through ROOT's client
// is answered at /mcp with the node's tools, and not at a namespace's server.
func TestROOTsClientIsAnsweredAtTheNodesMCPServer(t *testing.T) {
	const clientDID = "did:key:zrootsclient"
	tokens := &rootsClientTokens{heldTokens: heldTokens{grants: map[string]auth.Grant{}}, client: clientDID}
	raw := "qntx_test_through_roots_client"
	sum := sha256.Sum256([]byte(raw))
	tokens.grants[hex.EncodeToString(sum[:])] = auth.Grant{
		MintedBy: rootAccount, Level: auth.LevelRoot, ClientDID: clientDID, DID: "did:key:zissued", Label: "chatgpt",
	}

	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, testUsers(t), false, []string{rootAccount}, nil)
	require.NoError(t, err)
	logger := zaptest.NewLogger(t).Sugar()
	srv := &QNTXServer{
		pluginRegistry: plugin.NewRegistry("0.1.0", logger),
		authHandler:    h,
		authEnabled:    true,
		logger:         logger,
		rlAuth:         newRateLimitGroup(100, 100),
		rlWS:           newRateLimitGroup(100, 100),
		rlWrite:        newRateLimitGroup(100, 100),
		rlRead:         newRateLimitGroup(100, 100),
		rlPublic:       newRateLimitGroup(100, 100),
		held:           servingStub(&stubStore{}),
	}
	srv.setupHTTPRoutes()
	require.NoError(t, srv.open())

	node := httptest.NewServer(srv.served)
	t.Cleanup(node.Close)
	carrying := &http.Client{Transport: bearing{token: raw}}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "chatgpt", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: node.URL + "/mcp", HTTPClient: carrying}, nil)
	require.NoError(t, err, "the node's MCP server did not take ROOT's client")
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	// A namespace tool, which no connector is offered:
	//
	// "as ROOT, everything ROOT reaches"
	assert.True(t, names["namespaces_list"], "ROOT's client was not offered what ROOT reaches")

	asked, err := http.NewRequest(http.MethodPost, node.URL+"/mcp/default", strings.NewReader(`{}`))
	require.NoError(t, err)
	answered, err := carrying.Do(asked)
	require.NoError(t, err)
	_ = answered.Body.Close()
	assert.Equal(t, http.StatusNotFound, answered.StatusCode, "a namespace's MCP server answered ROOT's client")
}

// rootsClientTokens holds ROOT's client, listed the way the token store lists it.
type rootsClientTokens struct {
	heldTokens
	client string
}

func (r *rootsClientTokens) List() ([]auth.TokenInfo, error) {
	return []auth.TokenInfo{{DID: r.client, Label: "chatgpt", Level: auth.LevelOAuth, MintedBy: rootAccount, Root: true}}, nil
}

// bearing is a client carrying the token on every request, as an app does.
type bearing struct{ token string }

func (b bearing) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
