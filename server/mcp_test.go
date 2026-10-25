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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap/zaptest"
)

// "a new thing is a new handler is a new mcp tool is a new api endpoint"
//
// "no handrolled tools"
//
// The tools are the sigils, and beside them every route the node serves that
// no sigil answers yet and that asks who is calling. Nothing else is a tool,
// and no document is read.
func TestTheToolsAreTheSigilsAndTheRoutesServed(t *testing.T) {
	srv := servedForTest(t)

	var expected []string
	for _, signum := range srv.signa() {
		for _, held := range signum.GetSigils() {
			expected = append(expected, toolNameOf(signum.GetName(), held))
		}
	}
	for _, route := range srv.served.Routes() {
		if _, anyone := srv.served.Reaching(route.Path); routeTool(route) && !anyone {
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

	// "served without asking who is calling" (reach/table.go): a door, a
	// discovery document, a receive point, UI. Whoever calls a tool was asked.
	for _, door := range []string{"http_auth_login_begin", "http_auth_register_finish", "http_auth_token",
		"http__well_known_oauth_protected_resource", "http_setup_claim", "http_s_", "http_g_", "http_github_", "http_health", "http_"} {
		assert.False(t, named[door], door+" is served to anyone, and offered as a tool")
	}
	assert.True(t, named["http_auth_tokens"], "the tokens ROOT and SUPER reach are not a tool")
}

// askingFor is an MCP client of the tools srv offers.
func askingFor(t *testing.T, srv *QNTXServer) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := srv.mcpServerFor(httptest.NewRequest(http.MethodPost, "/mcp/", nil))
	clientSide, serverSide := mcp.NewInMemoryTransports()
	serving, err := server.Connect(ctx, serverSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serving.Close() })
	asking, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asking.Close() })
	return asking
}

// Nothing says which methods a route no sigil answers takes, so a call that
// names none is refused before anything is asked.
func TestARouteToolAskedWithoutAMethodIsRefused(t *testing.T) {
	result, err := askingFor(t, servedForTest(t)).CallTool(context.Background(), &mcp.CallToolParams{Name: "http_api_types", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, textOf(t, result), "does not take what was sent")
	assert.Contains(t, textOf(t, result), "method")
}

// What a tool says it takes is what it takes: a call is held to its
// inputSchema before anything is asked, so a number is not text and a method
// is one of those it names.
func TestAToolIsAskedOnlyWhatItSaysItTakes(t *testing.T) {
	asking := askingFor(t, servedForTest(t))
	for name, args := range map[string]map[string]any{
		"parity_hold":    {"signum": 5},
		"http_api_types": {"method": "FETCH"},
	} {
		result, err := asking.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		assert.True(t, result.IsError, name+" was asked what it does not take")
		assert.Contains(t, textOf(t, result), "invalid (arguments): "+name+" does not take what was sent")
	}
	held, err := asking.CallTool(context.Background(), &mcp.CallToolParams{Name: "parity_hold", Arguments: map[string]any{"signum": "parity", "reference": "mcp"}})
	require.NoError(t, err)
	assert.False(t, held.IsError, textOf(t, held))
}

// A field that names the message it carries is said in that message's shape,
// as the answer marshals it: one, a list, or none, each field of its kind.
func TestAFieldNamingItsMessageIsSaidInItsShape(t *testing.T) {
	gives := promisedBy(t, &protocol.Sigil{Gives: []*protocol.Field{
		{Name: "visits", Says: "The sittings.", Message: "protocol.Visit"},
		{Name: "transcript", Says: "The session.", Message: "protocol.Transcript"},
		{Name: "market", Says: "The market."},
	}}).schema
	carried, err := json.Marshal(map[string]any{
		"visits": []*protocol.Visit{{Visit: "v", Visitor: "p", DurationSeconds: 3, Views: 2, Bounce: true}},
		"transcript": &protocol.Transcript{Session: "s", Subjects: []string{"a"}, Folded: 4,
			Turns: []*protocol.Turn{{At: "t", Speaker: "person", Text: "hey"}}},
		"market": 7,
	})
	require.NoError(t, err)
	assert.NoError(t, heldTo(gives, carried), "what the answer marshals is not what the tool says it gives")
	assert.NoError(t, heldTo(gives, []byte(`{"visits":null}`)), "none is what a field may carry")
	assert.Error(t, heldTo(gives, []byte(`{"visits":[{"views":"2"}]}`)), "a count given as text")
	assert.Error(t, heldTo(gives, []byte(`{"transcript":{"turns":[{"text":1}]}}`)), "a turn's text given as a number")
}

// promisedBy is what a sigil says it gives, as a tool says it.
func promisedBy(t *testing.T, held *protocol.Sigil) promise {
	t.Helper()
	promised, err := givenAsSchema(held)
	require.NoError(t, err)
	return promised
}

// A sigil naming its answer gives that message, every field of it there and
// zero as zero: what answerJSON writes is what the tool says it gives, and an
// answer leaving a field out is not.
func TestAnAnswerIsItsMessageWhole(t *testing.T) {
	promised := promisedBy(t, &protocol.Sigil{Answer: "protocol.Transcripts"})
	require.True(t, promised.says)

	written, err := answerJSON(&protocol.Transcripts{Transcripts: []*protocol.Transcript{{Session: "s", Turns: []*protocol.Turn{{Text: "hey"}}}}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"transcripts":[{"session":"s","subjects":[],"started":"","ended":"","turns":[{"at":"","speaker":"","text":"hey","of":""}],"folded":0,"model":"","effort":""}]}`, string(written))
	assert.NoError(t, heldTo(promised.schema, written))

	assert.Error(t, heldTo(promised.schema, []byte(`{"transcripts":[{"session":"s"}]}`)), "a session leaving out what it holds")
	assert.Error(t, heldTo(promised.schema, []byte(`{"transcripts":[{"session":"s","subjects":[],"started":"","ended":"","turns":[],"folded":"0","model":"","effort":""}]}`)), "a count given as text")

	written, err = answerJSON(&protocol.SessionTranscript{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"transcript":null}`, string(written))
	assert.NoError(t, heldTo(promisedBy(t, &protocol.Sigil{Answer: "protocol.SessionTranscript"}).schema, written), "a message not set is null")
}

// What a sigil naming its answer gives is that message's fields in its .proto's
// words, said nowhere else. Naming the answer and listing the fields as well is
// saying it twice, and a message this binary does not know is no answer.
func TestWhatASigilGivesIsItsAnswer(t *testing.T) {
	signum := func(held *protocol.Sigil) sigil.Signum {
		held.Name, held.Does = "version", "Which build."
		held.Http = &protocol.Endpoint{Method: http.MethodGet, Path: "/am/version"}
		return sigil.Signum{Signum: &protocol.Signum{Name: "am", Sigils: []*protocol.Sigil{held}},
			Answers: map[string]sigil.Answer{"version": func(context.Context, sigil.Sent) (any, *protocol.Refusal) { return &protocol.VersionInfo{}, nil }}}
	}
	answered, err := answeredOf(signum(&protocol.Sigil{Answer: "protocol.VersionInfo"}))
	require.NoError(t, err)
	require.NoError(t, answered.Check())
	var gives []string
	for _, field := range answered.GetSigils()[0].GetGives() {
		gives = append(gives, field.GetName()+": "+field.GetSays())
	}
	assert.Equal(t, []string{
		"commit_hash: The whole commit.", "build_time: When it was built.", "version: The version tag.",
		"go_version: The Go it was built with.", "platform: The OS and architecture.",
	}, gives)

	_, err = answeredOf(signum(&protocol.Sigil{Answer: "protocol.Nosuch"}))
	assert.ErrorContains(t, err, "no message this binary knows")
}

// A sigil naming its answer says what it gives there and nowhere else: none of
// the node's own lists the fields by hand as well.
func TestNoSigilSaysWhatItGivesTwice(t *testing.T) {
	for _, signum := range servedForTest(t).signa() {
		for _, held := range signum.GetSigils() {
			if held.GetAnswer() != "" {
				assert.Empty(t, held.GetGives(), "%s %s names %s and lists what it gives as well", signum.GetName(), held.GetName(), held.GetAnswer())
			}
		}
	}
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
	bareNode().HandleMCP(w, req)

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

// mcpAskedAs is one MCP request to path, as admitted.
func mcpAskedAs(admitted auth.Admission, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req = req.WithContext(auth.WithAdmission(req.Context(), admitted))
	w := httptest.NewRecorder()
	bareNode().HandleMCP(w, req)
	return w
}

// "I actually think this part should be split up into, Node MCP server and Namespace MCP server's"
//
// A connector answers at the MCP server of the namespace its client was minted
// in, and the refusal anywhere else says where that is.
func TestAConnectorAnswersAtItsNamespacesMCPServer(t *testing.T) {
	connector := auth.Admitted(auth.LevelConnector, "Clean")
	connector.ClientDID = "did:key:zconnector"

	at := mcpAskedAs(connector, "/mcp/Clean")
	assert.NotEqual(t, http.StatusNotFound, at.Code, at.Body.String())
	for _, elsewhere := range []string{"/mcp", "/mcp/", "/mcp/default"} {
		refused := mcpAskedAs(connector, elsewhere)
		assert.Equal(t, http.StatusNotFound, refused.Code, elsewhere)
		assert.Contains(t, refused.Body.String(), "/mcp/Clean", elsewhere)
	}
}

// The node's MCP server is /mcp, for every caller that is no connector, as before.
func TestTheNodesMCPServerIsEveryCallerButAConnector(t *testing.T) {
	person := auth.Admitted(auth.LevelRoot)

	at := mcpAskedAs(person, "/mcp")
	assert.NotEqual(t, http.StatusNotFound, at.Code, at.Body.String())
	refused := mcpAskedAs(person, "/mcp/Clean")
	assert.Equal(t, http.StatusNotFound, refused.Code)
	assert.Contains(t, refused.Body.String(), "the node's MCP server is /mcp")
}

// A connector acts in one namespace (ADR-038): one naming none or several has
// no namespace MCP server to answer at.
func TestAConnectorNamingNoOneNamespaceIsRefused(t *testing.T) {
	for _, named := range [][]string{nil, {"Clean", "default"}} {
		connector := auth.Admitted(auth.LevelConnector, named...)
		connector.ClientDID = "did:key:zconnector"
		refused := mcpAskedAs(connector, "/mcp/Clean")
		assert.Equal(t, http.StatusForbidden, refused.Code, "%v", named)
	}
}

// "but i want a MCP specifically for the Node, only for ROOT"
func TestROOTsClientAnswersAtTheNodesMCPServer(t *testing.T) {
	client := auth.Admitted(auth.LevelRoot)
	client.ClientDID = "did:key:zroot"

	at := mcpAskedAs(client, "/mcp")
	assert.NotEqual(t, http.StatusNotFound, at.Code, at.Body.String())
	assert.NotEqual(t, http.StatusForbidden, at.Code, at.Body.String())
	refused := mcpAskedAs(client, "/mcp/Clean")
	assert.Equal(t, http.StatusNotFound, refused.Code)
	assert.Contains(t, refused.Body.String(), "the node's MCP server is /mcp")
}

// "the conenctor, to the connector, the namespace should be invisible"
func TestAConnectorIsOfferedNoNamespaceTool(t *testing.T) {
	srv := servedForTest(t)
	person := auth.Admitted(auth.LevelRoot)
	connector := auth.Admitted(auth.LevelConnector, auth.NamespaceDefault)
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
}

// A route that names a segment answers any path, so a path it would carry to a
// namespace is refused to a connector too.
func TestAConnectorCannotReachANamespacePathThroughAnotherTool(t *testing.T) {
	asked := false
	served := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true })
	connector := auth.Admitted(auth.LevelConnector, auth.NamespaceDefault)
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
		return overMCP(context.Background(), admits, everyone, httptest.NewRequest(http.MethodPost, "/mcp", nil), given, nil, promisedBy(t, given.sigil))
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

// "guide your agent is easy to add"
//
// A connector's MCP server tells the agent what its client says to, on
// initialize (ADR-038). Anyone else is told nothing.
func TestAConnectorIsToldItsClientsInstructions(t *testing.T) {
	_, db := createTestStore(t)
	tokens, _, err := auth.OpenTokenTable(db, nil)
	require.NoError(t, err)
	h, err := auth.New(db, "localhost", nil, 8770, 8820, 24, zaptest.NewLogger(t).Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, testUsers(t), false, []string{rootAccount}, nil)
	require.NoError(t, err)
	_, _, err = tokens.Create(auth.NewToken{
		Label: "app", MintedBy: rootAccount, Level: auth.LevelOAuth, ExpiresAt: auth.NeverEnds(),
		Namespaces: []string{auth.NamespaceDefault}, ReturnAddress: "https://app.example/callback",
		Instructions: "Always show drafts before sending.",
	})
	require.NoError(t, err)
	listed, err := tokens.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	srv := servedForTest(t)
	srv.authHandler = h

	told := func(admitted auth.Admission) string {
		asked := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
		asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
		return initializedFor(t, srv, asked).Instructions
	}
	connector := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	connector.ClientDID = listed[0].DID
	assert.Equal(t, "Always show drafts before sending.", told(connector))

	stranger := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	stranger.ClientDID = "did:key:znobody"
	assert.Equal(t, "", told(stranger), "a connector through no live client was told something")
	assert.Equal(t, "", told(auth.Admitted(auth.LevelRoot)), "a person was told a client's instructions")
}

// A connector is offered what the lines grant its client's label, and a
// connector whose label holds nothing is offered nothing (ADR-038, ADR-034):
//
// "DEFAULT DENY"
func TestAConnectorIsOfferedWhatItsLabelReaches(t *testing.T) {
	srv := servedForTest(t)
	picked := reach.Line{Paths: []string{"staands"}, Roles: []string{"APP"}, Actor: rootAccount, At: time.Now()}
	_, err := srv.served.Reopen(srv.answering, srv.wrapping(), reach.Runtime{
		Lines:  []reach.Line{picked},
		IsRoot: func(actor string) bool { return actor == rootAccount },
		Plugin: srv.pluginRoute,
	})
	require.NoError(t, err)

	offered := func(admitted auth.Admission) []string {
		asked := httptest.NewRequest(http.MethodPost, "/mcp/", nil)
		asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
		var names []string
		for _, tool := range toolsOfferedFor(t, srv, asked) {
			names = append(names, tool.Name)
		}
		return names
	}
	nobody := auth.Admitted(auth.LevelConnector, auth.NamespaceDefault)
	nobody.ClientDID = "did:key:zconnector"
	assert.Empty(t, offered(nobody), "a connector holding nothing was offered a tool")

	app := auth.Holding(nobody, "APP")
	tools := offered(app)
	require.NotEmpty(t, tools, "the line granting APP the staands signum offered nothing")
	for _, name := range tools {
		assert.True(t, strings.HasPrefix(name, "staands_"), name+" is not staands', and APP holds staands alone")
	}
}

// initializedFor is what the client learns on initialize from the server one
// request would be answered by.
func initializedFor(t *testing.T, s *QNTXServer, asked *http.Request) *mcp.InitializeResult {
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

	return asking.InitializeResult()
}
