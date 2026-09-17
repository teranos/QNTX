package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An MCP client is offered the two languages QNTX already has and nothing
// beside them (ADR-038): ax to ask, as to say.
func TestTheMCPServerOffersAXAndAS(t *testing.T) {
	listed := toolsOffered(t, &QNTXServer{})

	named := make([]string, 0, len(listed))
	for _, tool := range listed {
		named = append(named, tool.Name)
	}
	assert.ElementsMatch(t, []string{"ax", "as"}, named,
		"the tool surface is the query language, not a vocabulary beside it")
}

// A tool a client cannot read the shape of is a tool it cannot call, so both
// carry a description and an input schema.
func TestBothToolsSayWhatTheyTake(t *testing.T) {
	for _, tool := range toolsOffered(t, &QNTXServer{}) {
		assert.NotEmpty(t, tool.Description, tool.Name+" says nothing about what it is")
		assert.NotNil(t, tool.InputSchema, tool.Name+" says nothing about what it takes")
	}
}

// The node listens on loopback behind the proxy, so a request arrives on
// 127.0.0.1 naming the public host.
func TestAProxiedMCPRequestIsNotRefusedAsRebinding(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp/", strings.NewReader(`{}`))
	req.Host = "api.node.test"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8770}))

	w := httptest.NewRecorder()
	(&QNTXServer{}).HandleMCP(w, req)

	assert.NotEqual(t, http.StatusForbidden, w.Code, w.Body.String())
}

// toolsOffered connects a client to the server one request would be answered
// by, over the transport the SDK provides for exactly this, and lists what it
// finds.
func toolsOffered(t *testing.T, s *QNTXServer) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()

	server := s.mcpServerFor(httptest.NewRequest(http.MethodPost, "/mcp/", nil))
	clientSide, serverSide := mcp.NewInMemoryTransports()

	serving, err := server.Connect(ctx, serverSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serving.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	asking, err := client.Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asking.Close() })

	found, err := asking.ListTools(ctx, nil)
	require.NoError(t, err)
	return found.Tools
}
