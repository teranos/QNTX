package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
)

// sigilPlugin is fakePlugin handing the node signa, answering what a sigil
// hands it with whatever the test set, and keeping what it was handed.
type sigilPlugin struct {
	fakePlugin
	signa  []*protocol.Signum
	answer *protocol.HTTPResponse
	handed []*protocol.HTTPRequest
	// during is what the plugin does while the call is open, before answering.
	during func(*protocol.HTTPRequest)
}

func (p *sigilPlugin) GetSigna() []*protocol.Signum { return p.signa }

func (p *sigilPlugin) AnswerHTTP(_ context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error) {
	p.handed = append(p.handed, req)
	if p.during != nil {
		p.during(req)
	}
	return p.answer, nil
}

// headerOf is one header of a request a plugin was handed.
func headerOf(req *protocol.HTTPRequest, name string) []string {
	for _, h := range req.GetHeaders() {
		if h.GetName() == name {
			return h.GetValues()
		}
	}
	return nil
}

// stub's own shape: a read that takes a kind out of a few, and an observe
// that takes a body.
func stubSignum(name string) *protocol.Signum {
	return &protocol.Signum{
		Name: name,
		Sigils: []*protocol.Sigil{
			{
				Name: "read",
				Does: "What is observed of a kind.",
				Takes: []*protocol.Param{
					{Name: "kind", Says: "The kind.", Required: true, OneOf: []string{"competitor"}},
					{Name: "name", Says: "One subject."},
				},
				Gives: []*protocol.Field{{Name: "observed", Says: "Whether it is."}},
				Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/" + name + "/read"},
			},
			{
				Name: "observe",
				Does: "Write down what was seen.",
				Takes: []*protocol.Param{
					{Name: "kind", Says: "The kind.", Required: true},
					{Name: "value", Says: "What was seen.", Required: true},
				},
				Gives: []*protocol.Field{{Name: "observed", Says: "Whether it is."}},
				Http:  &protocol.Endpoint{Method: http.MethodPost, Path: "/api/" + name + "/observe"},
			},
		},
	}
}

// sigilServingServer is pluginServingServer with a plugin that hands the node
// signa, served the way cmd/qntx/main.go serves them once Initialize is done.
func sigilServingServer(t *testing.T, p *sigilPlugin) (*QNTXServer, map[auth.Level]string) {
	t.Helper()
	srv, tokens := pluginServingServer(t, "other")
	require.NoError(t, srv.pluginRegistry.Register(p))
	srv.pluginRegistry.MarkReady(p.name)
	srv.RegisterPluginMux(p.name)
	return srv, tokens
}

// "a plugin hands the node a Signum to say what it can do". Its sigils are
// served on their paths as the node's own are: what was sent is read against
// what the sigil takes before the plugin is asked, the plugin is handed the
// path below /api/{plugin}, and what it answers is held to what the sigil gives.
func TestAPluginsSigilIsServedOnItsPath(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=competitor&name=acme", tokens[auth.LevelRoot]))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"observed":true}`, w.Body.String())
	require.Len(t, p.handed, 1)
	assert.Equal(t, http.MethodGet, p.handed[0].GetMethod())
	assert.Equal(t, "/read?kind=competitor&name=acme", p.handed[0].GetPath())

	// A body rides as a JSON object of what the sigil takes, and nothing else.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stub/observe",
		strings.NewReader(`{"kind":"competitor","value":"true","extra":"dropped"}`))
	req.Header.Set("Authorization", "Bearer "+tokens[auth.LevelRoot])
	srv.served.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, p.handed, 2)
	assert.Equal(t, "/observe", p.handed[1].GetPath())
	assert.JSONEq(t, `{"kind":"competitor","value":"true"}`, string(p.handed[1].GetBody()))

	// Refused by the sigil, the plugin is never asked.
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=supplier", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "read takes kind as one of competitor")
	assert.Len(t, p.handed, 2, "the plugin was asked something its sigil refuses")

	// An answer that carries what the sigil never said it gives is the plugin
	// failing, and the caller is told only that it did.
	p.answer = &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true,"secret":"x"}`)}
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=competitor", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "secret")

	// The plugin says no in the sigil's terms, and the caller gets it as the
	// node's own sigils give theirs.
	p.answer = &protocol.HTTPResponse{StatusCode: http.StatusBadRequest,
		Body: []byte(`{"why":"not one of","param":"name","says":"acme is not a subject of competitor"}`)}
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=competitor&name=acme", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "acme is not a subject of competitor")

	// A plugin sigil is ROOT's until a line opens it, as any plugin path is.
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=competitor", tokens[auth.LevelSuper]))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Who is asking reaches the plugin as the node admitted them, and a caller
// saying otherwise is not what arrives.
func TestAPluginIsToldWhoIsAsking(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)

	req := asBearer(http.MethodGet, "/api/stub/read?kind=competitor", tokens[auth.LevelRoot])
	req.Header.Set(HeaderAsker, "did:key:somebody-else")
	srv.served.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, p.handed, 1)
	assert.Equal(t, []string{rootAccount}, headerOf(p.handed[0], HeaderAsker))
}

// A plugin answering a sigil reads and writes where its caller acts, through
// the token the node hands it for that call, and the token ends with the call.
func TestAPluginIsHandedTheStoreOfItsCallersNamespace(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)
	var reached ats.AttestationStore
	var open bool
	p.during = func(req *protocol.HTTPRequest) {
		token := headerOf(req, HeaderStoreToken)
		require.Len(t, token, 1)
		reached, open = srv.storeOfCall(token[0])
	}

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/stub/read?kind=competitor", tokens[auth.LevelRoot]))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, p.handed, 1)
	assert.True(t, open, "the token handed for the call reached no store while the call was open")
	assert.Same(t, srv.held.Served(), reached, "the call reached another store than the one its caller acts in")
	_, still := srv.storeOfCall(headerOf(p.handed[0], HeaderStoreToken)[0])
	assert.False(t, still, "the call's token outlived the call")
}

// A plugin's sigil is one tool, as the node's are, and asked over MCP it is
// the plugin that answers.
func TestAPluginsSigilIsATool(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":false}`)},
	}
	srv, _ := sigilServingServer(t, p)

	named := map[string]*mcp.Tool{}
	for _, tool := range toolsOffered(t, srv) {
		named[tool.Name] = tool
	}
	require.Contains(t, named, "stub_read")
	require.Contains(t, named, "stub_observe")
	assert.Equal(t, "What is observed of a kind.", named["stub_read"].Description)

	raw, err := json.Marshal(named["stub_read"].InputSchema)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"enum":["competitor"]`)

	var read heldBy
	for _, signum := range srv.checkedSigna() {
		for _, held := range signum.GetSigils() {
			if signum.GetName() == "stub" && held.GetName() == "read" {
				read = heldBy{signum: "stub", sigil: held, answer: signum.Answers["read"]}
			}
		}
	}
	require.NotNil(t, read.sigil)
	admits := func(_ string, _ auth.Reach, next http.HandlerFunc) http.HandlerFunc { return next }
	everyone := func(string, heldBy) (auth.Reach, bool) { return auth.Reach{}, true }
	answered := overMCP(context.Background(), admits, everyone, httptest.NewRequest(http.MethodPost, "/mcp", nil), read,
		map[string]any{"kind": "competitor"})
	require.False(t, answered.IsError, textOf(t, answered))
	assert.JSONEq(t, `{"observed":false}`, textOf(t, answered))
}

// A plugin's signum is its own: named after it and bound under /api/{plugin}/.
// One that is not is served nowhere, because a line naming a signum or a path
// would otherwise reach it on another's behalf.
func TestAPluginsSignumThatIsNotItsOwnIsNotServed(t *testing.T) {
	for name, handed := range map[string]*protocol.Signum{
		"named after another": stubSignum("staands"),
		"bound outside its paths": {Name: "stub", Sigils: []*protocol.Sigil{{
			Name: "list", Does: "Lists.", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/everything"},
		}}},
		"bound to its root": {Name: "stub", Sigils: []*protocol.Sigil{{
			Name: "list", Does: "Lists.", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/stub/"},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := &sigilPlugin{fakePlugin: fakePlugin{name: "stub"}, signa: []*protocol.Signum{handed}}
			srv, _ := sigilServingServer(t, p)
			for _, signum := range srv.checkedSigna() {
				assert.NotSame(t, handed, signum.Signum, "a signum that is not the plugin's own was served")
			}
			assert.NotContains(t, srv.answering, "/api/staands/everything")
		})
	}
}

// The panel is told each sigil and who reaches it from the lines the gate is
// given, and a refused signum is told why rather than only logged.
func TestThePanelIsToldEachSigilAndWhoReachesIt(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
	}
	srv, _ := sigilServingServer(t, p)

	rows, refused := srv.pluginSigilRows("stub")
	assert.Empty(t, refused)
	require.Len(t, rows, 2)
	read := rows[0]
	assert.Equal(t, "stub", read.Signum)
	assert.Equal(t, "read", read.Sigil)
	assert.Equal(t, "stub_read", read.Tool)
	assert.Equal(t, http.MethodGet, read.Method)
	assert.Equal(t, "/api/stub/read", read.Path)
	assert.Equal(t, []string{"competitor"}, read.Takes[0].GetOneOf())
	for _, surface := range []string{"http", "mcp"} {
		assert.Equal(t, reached{Levels: []string{}, Roles: []string{}}, read.Reach[surface],
			"a plugin sigil no line opens is ROOT's only, over "+surface)
	}

	p.signa = []*protocol.Signum{stubSignum("staands")}
	rows, refused = srv.pluginSigilRows("stub")
	assert.Empty(t, rows)
	require.Len(t, refused, 1)
	assert.Contains(t, refused[0], "handed a signum named staands")
}

// A plugin that restarts may hand different signa, and a path no sigil is
// bound to any more is not answered as one.
func TestASigilAPluginNoLongerHandsIsNotServed(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "stub"},
		signa:      []*protocol.Signum{stubSignum("stub")},
	}
	srv, _ := sigilServingServer(t, p)
	require.True(t, srv.answering["/api/stub/read"].Gates)

	p.signa = nil
	srv.ServePluginSigils()
	_, answered := srv.answering["/api/stub/read"]
	assert.False(t, answered, "a sigil the plugin no longer hands is still answered")
}

// A line naming a signum reaches its sigils wherever the plugin bound them,
// over both surfaces. datapunt's line is the one in the table (ADR-039).
func TestASignumTheTableNamesIsReachedBySuper(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{stubSignum("datapunt")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)

	rows, refused := srv.pluginSigilRows("datapunt")
	require.Empty(t, refused)
	require.Len(t, rows, 2)
	for _, surface := range []string{"http", "mcp"} {
		assert.Equal(t, []string{"SUPER"}, rows[0].Reach[surface].Levels, "over "+surface)
	}

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor", tokens[auth.LevelSuper]))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"observed":true}`, w.Body.String())
}
