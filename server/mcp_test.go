package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "a new thing is a new handler is a new mcp tool is a new api endpoint"
//
// "no handrolled tools"
//
// The tools are the sigils, and beside them every route the node serves that
// no sigil answers yet. Nothing else is a tool, and no document is read.
func TestTheToolsAreTheSigilsAndTheRoutesServed(t *testing.T) {
	srv := servedForTest(t)

	var expected []string
	for _, signum := range srv.signa() {
		for _, held := range signum.GetSigils() {
			expected = append(expected, toolNameOf(signum.GetName(), held))
		}
	}
	for _, route := range srv.served.Routes() {
		if routeTool(route) {
			expected = append(expected, toolName(route.Path))
		}
	}

	named := map[string]bool{}
	var offered []string
	for _, tool := range toolsOffered(t, srv) {
		named[tool.Name] = true
		offered = append(offered, tool.Name)
	}
	assert.ElementsMatch(t, expected, offered, "a tool that is not a sigil or a route, or one that is not a tool")

	assert.True(t, named["http_api_attestations"], "asking and attesting are not a tool")
	assert.True(t, named["staands_metrics"], "a sigil is not a tool")
	assert.False(t, named["http_api_staands_metrics"], "a path sigils answer is offered twice")
	assert.False(t, named["http_ws"], "a socket is not something a tool call can hold open")
	assert.False(t, named["http_mcp"], "the MCP endpoint offers itself")
}

// Nothing says which methods a route no sigil answers takes, so a call that
// names none is refused before anything is asked.
func TestARouteToolAskedWithoutAMethodIsRefused(t *testing.T) {
	ctx := context.Background()
	server := servedForTest(t).mcpServerFor(httptest.NewRequest(http.MethodPost, "/mcp/", nil))
	clientSide, serverSide := mcp.NewInMemoryTransports()
	serving, err := server.Connect(ctx, serverSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serving.Close() })
	asking, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asking.Close() })

	result, err := asking.CallTool(ctx, &mcp.CallToolParams{Name: "http_api_roles", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, textOf(t, result), "needs a method")
}

// A tool a client cannot read the shape of is a tool it cannot call.
func TestEveryToolSaysWhatItIsAndWhatItTakes(t *testing.T) {
	for _, tool := range toolsOffered(t, servedForTest(t)) {
		assert.NotEmpty(t, tool.Description, tool.Name+" says nothing about what it is")
		assert.NotNil(t, tool.InputSchema, tool.Name+" says nothing about what it takes")
	}
}

// A tool call is a request on the served API, carrying the caller's own
// credential, so the gate that admits it is the gate every request meets.
func TestAToolCallIsARequestOnTheServedAPI(t *testing.T) {
	var arrived *http.Request
	var body string
	served := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived = r
		read, _ := io.ReadAll(r.Body)
		body = string(read)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"AS-1"}`))
	})
	asked := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	asked.Header.Set("Authorization", "Bearer qntx_caller")

	result := callThrough(context.Background(), served, asked,
		operation{Path: "/api/attestations", Method: http.MethodPost},
		calledThrough{Query: map[string]string{"namespace": "default"}, Body: json.RawMessage(`{"subjects":["tim"]}`)})

	require.NotNil(t, arrived, "the served API was never asked")
	assert.Equal(t, http.MethodPost, arrived.Method)
	assert.Equal(t, "/api/attestations", arrived.URL.Path)
	assert.Equal(t, "default", arrived.URL.Query().Get("namespace"))
	assert.Equal(t, "Bearer qntx_caller", arrived.Header.Get("Authorization"))
	assert.Equal(t, `{"subjects":["tim"]}`, body)
	assert.False(t, result.IsError)
	assert.Equal(t, `{"id":"AS-1"}`, textOf(t, result))
}

// What the API refuses, the tool refuses, in the API's own words.
func TestARefusalComesBackAsTheAPISaidIt(t *testing.T) {
	served := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "this route is not yours", http.StatusForbidden)
	})
	asked := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	result := callThrough(context.Background(), served, asked,
		operation{Path: "/api/roles", Method: http.MethodGet}, calledThrough{})

	assert.True(t, result.IsError)
	assert.Contains(t, textOf(t, result), "403")
	assert.Contains(t, textOf(t, result), "this route is not yours")
}

// A tool is one route. A path outside it is another tool's, and is refused
// before anything is asked.
func TestAPathOutsideTheToolsRouteIsRefused(t *testing.T) {
	asked := false
	served := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true })
	caller := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	exact := callThrough(context.Background(), served, caller,
		operation{Path: "/api/attestations", Method: http.MethodGet}, calledThrough{Path: "/api/roles"})
	under := callThrough(context.Background(), served, caller,
		operation{Path: "/api/types/", Method: http.MethodGet, Prefix: true}, calledThrough{Path: "/api/types/pond"})

	assert.True(t, exact.IsError, "a path another tool answers was called")
	assert.False(t, under.IsError, "a path under a prefix route was refused")
	assert.True(t, asked, "the path under the prefix route never arrived")
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

// A connector is given the URL without the slash. A redirect to `/mcp/` is
// followed as a GET, so the POST carrying initialize never arrives.
func TestAnMCPCallWithoutTheSlashIsAnswered(t *testing.T) {
	srv := servedForTest(t)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`)))

	assert.NotEqual(t, http.StatusMovedPermanently, w.Code, "redirected to %s", w.Header().Get("Location"))
}

func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "the result is %T", result.Content[0])
	return text.Text
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
