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
}

func (p *sigilPlugin) GetSigna() []*protocol.Signum { return p.signa }

func (p *sigilPlugin) AnswerHTTP(_ context.Context, req *protocol.HTTPRequest) (*protocol.HTTPResponse, error) {
	p.handed = append(p.handed, req)
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

// datapunt's own shape: a read that takes a kind out of a few, and an observe
// that takes a body.
func datapuntSignum(name string) *protocol.Signum {
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
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{datapuntSignum("datapunt")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor&name=acme", tokens[auth.LevelRoot]))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"observed":true}`, w.Body.String())
	require.Len(t, p.handed, 1)
	assert.Equal(t, http.MethodGet, p.handed[0].GetMethod())
	assert.Equal(t, "/read?kind=competitor&name=acme", p.handed[0].GetPath())

	// A body rides as a JSON object of what the sigil takes, and nothing else.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/datapunt/observe",
		strings.NewReader(`{"kind":"competitor","value":"true","extra":"dropped"}`))
	req.Header.Set("Authorization", "Bearer "+tokens[auth.LevelRoot])
	srv.served.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, p.handed, 2)
	assert.Equal(t, "/observe", p.handed[1].GetPath())
	assert.JSONEq(t, `{"kind":"competitor","value":"true"}`, string(p.handed[1].GetBody()))

	// Refused by the sigil, the plugin is never asked.
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=supplier", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "read takes kind as one of competitor")
	assert.Len(t, p.handed, 2, "the plugin was asked something its sigil refuses")

	// An answer that carries what the sigil never said it gives is the plugin
	// failing, and the caller is told only that it did.
	p.answer = &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true,"secret":"x"}`)}
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "secret")

	// The plugin says no in the sigil's terms, and the caller gets it as the
	// node's own sigils give theirs.
	p.answer = &protocol.HTTPResponse{StatusCode: http.StatusBadRequest,
		Body: []byte(`{"why":"not one of","param":"name","says":"acme is not a subject of competitor"}`)}
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor&name=acme", tokens[auth.LevelRoot]))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "acme is not a subject of competitor")

	// A plugin sigil is ROOT's until a line opens it, as any plugin path is.
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor", tokens[auth.LevelSuper]))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Who is asking reaches the plugin as the node admitted them, and a caller
// saying otherwise is not what arrives.
func TestAPluginIsToldWhoIsAsking(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{datapuntSignum("datapunt")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":true}`)},
	}
	srv, tokens := sigilServingServer(t, p)

	req := asBearer(http.MethodGet, "/api/datapunt/read?kind=competitor", tokens[auth.LevelRoot])
	req.Header.Set(HeaderAsker, "did:key:somebody-else")
	srv.served.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, p.handed, 1)
	assert.Equal(t, []string{rootAccount}, headerOf(p.handed[0], HeaderAsker))
}

// A plugin's sigil is one tool, as the node's are, and asked over MCP it is
// the plugin that answers.
func TestAPluginsSigilIsATool(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{datapuntSignum("datapunt")},
		answer:     &protocol.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"observed":false}`)},
	}
	srv, _ := sigilServingServer(t, p)

	named := map[string]*mcp.Tool{}
	for _, tool := range toolsOffered(t, srv) {
		named[tool.Name] = tool
	}
	require.Contains(t, named, "datapunt_read")
	require.Contains(t, named, "datapunt_observe")
	assert.Equal(t, "What is observed of a kind.", named["datapunt_read"].Description)

	raw, err := json.Marshal(named["datapunt_read"].InputSchema)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"enum":["competitor"]`)

	var read heldBy
	for _, signum := range srv.checkedSigna() {
		for _, held := range signum.GetSigils() {
			if signum.GetName() == "datapunt" && held.GetName() == "read" {
				read = heldBy{signum: "datapunt", sigil: held, answer: signum.Answers["read"]}
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
		"named after another": datapuntSignum("staands"),
		"bound outside its paths": {Name: "datapunt", Sigils: []*protocol.Sigil{{
			Name: "list", Does: "Lists.", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/everything"},
		}}},
		"bound to its root": {Name: "datapunt", Sigils: []*protocol.Sigil{{
			Name: "list", Does: "Lists.", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/datapunt/"},
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := &sigilPlugin{fakePlugin: fakePlugin{name: "datapunt"}, signa: []*protocol.Signum{handed}}
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
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{datapuntSignum("datapunt")},
	}
	srv, _ := sigilServingServer(t, p)

	rows, refused := srv.pluginSigilRows("datapunt")
	assert.Empty(t, refused)
	require.Len(t, rows, 2)
	read := rows[0]
	assert.Equal(t, "datapunt", read.Signum)
	assert.Equal(t, "read", read.Sigil)
	assert.Equal(t, "datapunt_read", read.Tool)
	assert.Equal(t, http.MethodGet, read.Method)
	assert.Equal(t, "/api/datapunt/read", read.Path)
	assert.Equal(t, []string{"competitor"}, read.Takes[0].GetOneOf())
	for _, surface := range []string{"http", "mcp"} {
		assert.Equal(t, reached{Levels: []string{}, Roles: []string{}}, read.Reach[surface],
			"a plugin sigil no line opens is ROOT's only, over "+surface)
	}

	p.signa = []*protocol.Signum{datapuntSignum("staands")}
	rows, refused = srv.pluginSigilRows("datapunt")
	assert.Empty(t, rows)
	require.Len(t, refused, 1)
	assert.Contains(t, refused[0], "handed a signum named staands")
}

// A plugin that restarts may hand different signa, and a path no sigil is
// bound to any more is not answered as one.
func TestASigilAPluginNoLongerHandsIsNotServed(t *testing.T) {
	p := &sigilPlugin{
		fakePlugin: fakePlugin{name: "datapunt"},
		signa:      []*protocol.Signum{datapuntSignum("datapunt")},
	}
	srv, _ := sigilServingServer(t, p)
	require.True(t, srv.answering["/api/datapunt/read"].Gates)

	p.signa = nil
	srv.ServePluginSigils()
	_, answered := srv.answering["/api/datapunt/read"]
	assert.False(t, answered, "a sigil the plugin no longer hands is still answered")
}
