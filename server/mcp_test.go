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
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
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

	result, err := asking.CallTool(ctx, &mcp.CallToolParams{Name: "http_api_types", Arguments: map[string]any{}})
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

// "the conenctor, to the connector, the namespace should be invisible"
func TestAConnectorIsOfferedNoNamespaceTool(t *testing.T) {
	srv := servedForTest(t)
	person := auth.Admitted(auth.LevelRoot)
	connector := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	connector.ClientDID = "did:key:zconnector"

	offered := func(admitted auth.Admission) map[string]bool {
		asked := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
		asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
		named := map[string]bool{}
		for _, tool := range toolsOfferedFor(t, srv, asked) {
			named[tool.Name] = true
		}
		return named
	}
	toPerson, toConnector := offered(person), offered(connector)

	for _, namespaced := range []string{"i_standing", "i_step", "http_i_", "namespaces_list", "namespaces_delete"} {
		assert.True(t, toPerson[namespaced], namespaced+" is not offered to the person")
		assert.False(t, toConnector[namespaced], namespaced+" is offered to a connector")
	}
	assert.True(t, toConnector["http_api_attestations"], "a connector cannot ask or attest")
}

// A route that names a segment answers any path, so a path it would carry to a
// namespace is refused to a connector too.
func TestAConnectorCannotReachANamespacePathThroughAnotherTool(t *testing.T) {
	asked := false
	served := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true })
	connector := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	connector.ClientDID = "did:key:zconnector"
	caller := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	caller = caller.WithContext(auth.WithAdmission(caller.Context(), connector))

	result := callThrough(context.Background(), served, caller,
		operation{Path: "/api/plugins/{name}/config", Method: http.MethodGet}, calledThrough{Path: "/api/namespaces"})

	assert.True(t, result.IsError)
	assert.False(t, asked, "a connector reached a namespace path")
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
	return toolsOfferedFor(t, s, httptest.NewRequest(http.MethodPost, "/mcp/", nil))
}

// toolsOfferedFor is toolsOffered for one request, carrying whoever it carries.
func toolsOfferedFor(t *testing.T, s *QNTXServer, asked *http.Request) []*mcp.Tool {
	t.Helper()
	return listedFor(t, s, asked).Tools
}

// listedFor is the whole tools/list result one request is answered with.
func listedFor(t *testing.T, s *QNTXServer, asked *http.Request) *mcp.ListToolsResult {
	t.Helper()
	ctx := context.Background()

	server := s.mcpServerFor(asked)
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
	return found
}

// A caller is shown only what they reach, so the list one caller is given is
// theirs: no cache may hand it to another (cacheScope, 2026-07-28). A node
// with no login knows nobody and lists the same to everyone.
func TestAToolListIsTheCallersOwn(t *testing.T) {
	srv := servedForTest(t)
	asked := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
	assert.Equal(t, "public", listedFor(t, srv, asked).CacheScope, "a node that knows nobody")

	asked = asked.WithContext(auth.WithAdmission(asked.Context(), auth.Admitted(auth.LevelRoot)))
	assert.Equal(t, "private", listedFor(t, srv, asked).CacheScope, "a caller's list was cacheable for others")
}

// A tool says what its sigil promises: what it gives, as its outputSchema, and
// what its method promises, as hints. A GET is safe, a DELETE idempotent, and
// a POST promises neither.
func TestAToolSaysWhatItGivesAndWhatItsMethodPromises(t *testing.T) {
	named := map[string]*mcp.Tool{}
	for _, tool := range toolsOffered(t, servedForTest(t)) {
		named[tool.Name] = tool
	}

	hold := named["parity_hold"]
	require.NotNil(t, hold)
	require.NotNil(t, hold.Annotations)
	assert.True(t, hold.Annotations.ReadOnlyHint)
	schema, err := json.Marshal(hold.OutputSchema)
	require.NoError(t, err)
	for _, given := range []string{"clades", "unfollowed", "missing", "required"} {
		assert.Contains(t, string(schema), `"`+given+`"`, "outputSchema leaves out "+given)
	}

	takeDown := named["staands_take-down"]
	require.NotNil(t, takeDown)
	require.NotNil(t, takeDown.Annotations)
	assert.True(t, takeDown.Annotations.IdempotentHint)
	assert.False(t, takeDown.Annotations.ReadOnlyHint)

	assert.Nil(t, named["staands_create"].Annotations, "a POST promised something")
	assert.Nil(t, named["http_api_attestations"].OutputSchema, "a route no sigil answers said what it gives")
}

// What a tool says it gives is what it gives: the answer is the structured
// content, and an answer of another shape is the node's failure, not the
// caller's result. A refusal names its kind and its param.
func TestAToolGivesWhatItSaysItGives(t *testing.T) {
	var answer any
	given := heldBy{
		signum: "s",
		sigil: &protocol.Sigil{
			Name:  "read",
			Takes: []*protocol.Param{{Name: "kind", Says: "The kind."}},
			Gives: []*protocol.Field{{Name: "rows", Says: "The rows."}},
			Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/s"},
		},
		answer: func(context.Context, sigil.Sent) (any, *protocol.Refusal) {
			if answer == nil {
				return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "kind", Says: "no such kind"}
			}
			return answer, nil
		},
	}
	admits := func(_ string, _ auth.Reach, next http.HandlerFunc) http.HandlerFunc { return next }
	everyone := func(string, heldBy) (auth.Reach, bool) { return auth.Reach{}, true }
	ask := func() *mcp.CallToolResult {
		return overMCP(context.Background(), admits, everyone, httptest.NewRequest(http.MethodPost, "/mcp", nil), given, nil, true)
	}

	answer = map[string]any{"rows": []int{1}}
	answered := ask()
	require.False(t, answered.IsError, textOf(t, answered))
	structured, err := json.Marshal(answered.StructuredContent)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rows":[1]}`, string(structured))
	assert.JSONEq(t, `{"rows":[1]}`, textOf(t, answered))

	answer = []map[string]any{{"rows": 1}, {"rows": 2}}
	assert.False(t, ask().IsError, "a list of rows is what a sigil gives")

	answer = "rows"
	failed := ask()
	assert.True(t, failed.IsError)
	assert.Contains(t, textOf(t, failed), "is not what it says it gives")
	assert.Nil(t, failed.StructuredContent)

	answer = nil
	refusedByIt := ask()
	assert.True(t, refusedByIt.IsError)
	assert.Equal(t, "not found (kind): no such kind", textOf(t, refusedByIt))
}
