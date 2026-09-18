package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// sigilHTTP is the HTTP API's handler for one path sigils are bound to, the
// same one the mux serves.
func sigilHTTP(t *testing.T, s *QNTXServer, path string) http.HandlerFunc {
	t.Helper()
	answered, bound := s.answeredFromSigils()[path]
	require.True(t, bound, "no sigil is bound to "+path)
	return answered
}

// A path is answered from the sigils bound to it: the method picks the sigil,
// what was sent is read against what it takes, and only then is it asked.
func TestAPathIsAnsweredFromItsSigils(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())
	fire(s, "/s/clean/boutique?page=/&v=WHO-000001", "https://clean.example/")

	answering := s.answeredFromSigils()
	for _, path := range []string{"/api/staands", "/api/staands/metrics", "/api/staands/activity", "/api/staands/visits"} {
		require.Contains(t, answering, path)
	}

	metrics := answering["/api/staands/metrics"]

	asked := httptest.NewRecorder()
	metrics(asked, httptest.NewRequest(http.MethodGet, "/api/staands/metrics?market=clean&slug=boutique&type=page", nil))
	require.Equal(t, http.StatusOK, asked.Code, asked.Body.String())

	// The refusal the connector never got: it names type.
	missing := httptest.NewRecorder()
	metrics(missing, httptest.NewRequest(http.MethodGet, "/api/staands/metrics?market=clean&slug=boutique", nil))
	require.Equal(t, http.StatusBadRequest, missing.Code)
	require.Contains(t, missing.Body.String(), "metrics needs type")

	// A method no sigil is bound to is not answered, and the refusal says
	// which are.
	unbound := httptest.NewRecorder()
	metrics(unbound, httptest.NewRequest(http.MethodPost, "/api/staands/metrics", nil))
	require.Equal(t, http.StatusMethodNotAllowed, unbound.Code)
	require.Contains(t, unbound.Body.String(), "GET")

	// One path, three sigils: what arrives in a body is read the same way.
	stands := answering["/api/staands"]
	created := httptest.NewRecorder()
	stands(created, httptest.NewRequest(http.MethodPost, "/api/staands", strings.NewReader(`{"market":"clean","slug":"second"}`)))
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())

	slugless := httptest.NewRecorder()
	stands(slugless, httptest.NewRequest(http.MethodPost, "/api/staands", strings.NewReader(`{"market":"clean"}`)))
	require.Equal(t, http.StatusBadRequest, slugless.Code)
	require.Contains(t, slugless.Body.String(), "create needs slug")
}

// One path can hold sigils different people reach: listing the stands and
// taking one down are both /api/staands. Over HTTP each sigil is put behind the
// one gate with the lines about that sigil, somebody the gate turns away never
// reaches what answers, and a method no sigil is bound to is refused behind the
// gate too, with the row that admits ROOT alone.
func TestASigilIsGatedOverHTTPByTheLinesAboutIt(t *testing.T) {
	var answered []string
	does := func(name string) sigil.Answer {
		return func(context.Context, sigil.Sent) (any, *sigil.Refusal) {
			answered = append(answered, name)
			return map[string]any{"did": name}, nil
		}
	}
	bound := []heldBy{
		{signum: "staands", sigil: sigil.Sigil{Name: "list", Answer: does("list"),
			HTTP: sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands"}}},
		{signum: "staands", sigil: sigil.Sigil{Name: "take-down", Answer: does("take-down"),
			HTTP: sigil.Endpoint{Method: http.MethodDelete, Path: "/api/staands"}}},
	}
	reaching := func(held heldBy) (auth.Reach, bool) {
		if held.sigil.Name == "list" {
			return auth.Also(auth.LevelSuper, auth.LevelToken), false
		}
		return auth.Also(auth.LevelSuper), false
	}

	gated := map[string][]auth.Level{}
	admits := func(_ string, re auth.Reach, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gated[r.Method] = re.Beyond()
			next(w, r)
		}
	}
	stands := overHTTP("/api/staands", bound, admits, reaching, nil)

	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		stands(httptest.NewRecorder(), httptest.NewRequest(method, "/api/staands", nil))
	}
	assert.Equal(t, []string{"list", "take-down"}, answered)
	assert.Equal(t, []auth.Level{auth.LevelSuper, auth.LevelToken}, gated[http.MethodGet])
	assert.Equal(t, []auth.Level{auth.LevelSuper}, gated[http.MethodDelete], "taking down was gated with listing's line")
	assert.Empty(t, gated[http.MethodPut], "a method no sigil is bound to was gated for somebody beside ROOT")

	// Whoever the gate turns away reaches nothing, a sigil or the refusal that
	// says which methods the path answers.
	answered = nil
	turnsAway := func(string, auth.Reach, http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "this route is not yours", http.StatusForbidden)
		}
	}
	closed := overHTTP("/api/staands", bound, turnsAway, reaching, nil)
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		rec := httptest.NewRecorder()
		closed(rec, httptest.NewRequest(method, "/api/staands", nil))
		assert.Equal(t, http.StatusForbidden, rec.Code, method)
		assert.NotContains(t, rec.Body.String(), "answers", "somebody turned away was told what the path answers")
	}
	assert.Empty(t, answered, "a sigil answered somebody the gate turned away")
}

// "the tool is equivalent to 1 sigil each"
//
// A sigil's tool is named for the signum and the sigil, says what the sigil
// does, and takes what the sigil takes.
func TestASigilIsATool(t *testing.T) {
	named := map[string]*mcp.Tool{}
	for _, tool := range toolsOffered(t, &QNTXServer{}) {
		named[tool.Name] = tool
	}
	for _, name := range []string{"staands_list", "staands_create", "staands_take-down", "staands_metrics", "staands_activity", "staands_visits"} {
		require.Contains(t, named, name)
	}
	// The tools the document made for the same paths are gone: the sigil is the
	// one place, and one tool is one sigil.
	for _, name := range []string{"get_api_staands", "post_api_staands", "delete_api_staands", "get_api_staands_metrics"} {
		require.NotContains(t, named, name)
	}

	metrics := named["staands_metrics"]
	require.Equal(t, "One stand's arrivals grouped by one dimension, most first.", metrics.Description)

	raw, err := json.Marshal(metrics.InputSchema)
	require.NoError(t, err)
	var schema struct {
		Type       string `json:"type"`
		Required   []string
		Properties map[string]struct {
			Type        string
			Description string
			Enum        []string
		}
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	assert.Equal(t, "object", schema.Type)
	assert.ElementsMatch(t, []string{"market", "slug", "type"}, schema.Required)
	assert.Equal(t, staandDimensions(), schema.Properties["type"].Enum)
	assert.Equal(t, "What to group the arrivals by.", schema.Properties["type"].Description)
	assert.Contains(t, schema.Properties, "since")
}

// theGateAdmitted is how the test gate says who it let in, read by the sigil the way
// an admission is read in production: off the context the gate hands on.
type theGateAdmitted struct{}

// MCP is a surface of a sigil and not a caller of its endpoint: the arguments
// are what was sent, the answer is the sigil's own, and the sigil refuses in
// its own words. Who may ask is the one gate's to decide, with the row the
// lines give the sigil and the credential the MCP request carried.
func TestASigilIsAskedOverMCPBehindTheOneGate(t *testing.T) {
	var sentToIt sigil.Sent
	var askedBy any
	counts := sigil.Sigil{
		Name: "metrics",
		Takes: []sigil.Param{
			{Name: "market", Says: "The market.", Required: true},
			{Name: "type", Says: "What to group by.", Required: true, OneOf: []string{"page", "event"}},
			{Name: "limit", Says: "How many."},
		},
		HTTP: sigil.Endpoint{Method: http.MethodGet, Path: "/api/staands/metrics"},
		Answer: func(ctx context.Context, sent sigil.Sent) (any, *sigil.Refusal) {
			sentToIt, askedBy = sent, ctx.Value(theGateAdmitted{})
			return map[string]any{"type": sent["type"], "counts": []int{2, 1}}, nil
		},
	}
	caller := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	caller.Header.Set("Authorization", "Bearer qntx_caller")
	reaching := auth.Also(auth.LevelSuper)

	var gatedPath, gatedCredential string
	var gatedReach auth.Reach
	admits := func(path string, re auth.Reach, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gatedPath, gatedReach, gatedCredential = path, re, r.Header.Get("Authorization")
			next(w, r.WithContext(context.WithValue(r.Context(), theGateAdmitted{}, "tim")))
		}
	}
	turnsAway := func(string, auth.Reach, http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "this route is not yours", http.StatusForbidden)
		}
	}

	answered := askSigil(context.Background(), admits, reaching, false, caller, counts,
		map[string]any{"market": "clean", "type": "page", "limit": 5})
	require.False(t, answered.IsError, textOf(t, answered))
	assert.JSONEq(t, `{"type":"page","counts":[2,1]}`, textOf(t, answered))
	assert.Equal(t, sigil.Sent{"market": "clean", "type": "page", "limit": "5"}, sentToIt)
	assert.Equal(t, "tim", askedBy, "the sigil was not handed who the gate admitted")
	assert.Equal(t, "/api/staands/metrics", gatedPath)
	assert.Equal(t, reaching.Beyond(), gatedReach.Beyond())
	assert.Equal(t, "Bearer qntx_caller", gatedCredential)

	// The refusal the connector never got.
	sentToIt = nil
	refusedByIt := askSigil(context.Background(), admits, reaching, false, caller, counts, map[string]any{"market": "clean"})
	assert.True(t, refusedByIt.IsError)
	assert.Contains(t, textOf(t, refusedByIt), "metrics needs type")
	assert.Nil(t, sentToIt, "the sigil answered something it refuses")

	// Somebody the lines do not reach never reaches the sigil.
	refusedAtTheGate := askSigil(context.Background(), turnsAway, reaching, false, caller, counts,
		map[string]any{"market": "clean", "type": "page"})
	assert.True(t, refusedAtTheGate.IsError)
	assert.Contains(t, textOf(t, refusedAtTheGate), "this route is not yours")
	assert.Nil(t, sentToIt, "the sigil answered somebody the gate turned away")

	// A sigil the lines serve to anyone is asked without the gate.
	open := askSigil(context.Background(), turnsAway, auth.Reach{}, true, caller, counts,
		map[string]any{"market": "clean", "type": "event"})
	assert.False(t, open.IsError, textOf(t, open))
}

// A caller is shown only what they reach (ADR-039): a tool nobody could call
// is not something to list. The list asks the gate's own question of the row
// the gate will be given, so the two cannot disagree.
func TestACallerIsShownTheToolsTheyReach(t *testing.T) {
	forSupers := auth.Also(auth.LevelSuper)

	assert.True(t, offeredTo(auth.Admitted(auth.LevelRoot), true, forSupers, false), "ROOT reaches everything")
	assert.True(t, offeredTo(auth.Admitted(auth.LevelSuper), true, forSupers, false))
	assert.False(t, offeredTo(auth.Admitted(auth.LevelAttestor), true, forSupers, false),
		"a tool was shown to somebody its lines do not reach")
	assert.True(t, offeredTo(auth.Admitted(auth.LevelAttestor), true, auth.Reach{}, true),
		"what is served to anyone is shown to anyone")

	// A node with no login admits nobody because it asks nobody: every tool is
	// shown, as every route is served.
	assert.True(t, offeredTo(auth.Admission{}, false, auth.Reach{}, false))
}

// The document the node serves says a sigil's operations as the sigil says
// them. The written file cannot: its generator reads source, and a route
// offered from a sigil is not a literal there.
func TestTheServedDocumentSaysWhatTheSigilsSay(t *testing.T) {
	s := &QNTXServer{}
	raw, err := s.openapiServed()
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]struct {
			Summary    string   `json:"summary"`
			Sigil      string   `json:"x-qntx-sigil"`
			Reached    []string `json:"x-qntx-reach"`
			Parameters []struct {
				Name     string `json:"name"`
				In       string `json:"in"`
				Required bool   `json:"required"`
				Schema   struct {
					Enum []string `json:"enum"`
				} `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(raw, &document))

	stands := document.Paths["/api/staands"]
	require.Len(t, stands, 3, "list, create and take-down are the three things this path does")
	assert.Equal(t, "staands_create", stands["post"].Sigil)
	assert.Equal(t, "staands_take-down", stands["delete"].Sigil)

	metrics := document.Paths["/api/staands/metrics"]["get"]
	assert.Equal(t, "staands_metrics", metrics.Sigil)
	assert.Equal(t, []string{"ROOT", "SUPER"}, metrics.Reached, "who reaches it is the table's to say, and stays")
	var typed bool
	for _, param := range metrics.Parameters {
		if param.Name == "type" {
			typed = true
			assert.Equal(t, "query", param.In)
			assert.True(t, param.Required)
			assert.Equal(t, staandDimensions(), param.Schema.Enum)
		}
	}
	assert.True(t, typed, "the document does not say metrics takes type")
}
