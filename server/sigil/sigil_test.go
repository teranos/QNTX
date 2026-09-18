package sigil

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func answers(context.Context, Sent) (any, *Refusal) { return nil, nil }

// which is the one thing reading, updating and deleting a watcher all take.
var which = Param{Name: "id", Says: "Which watcher.", Required: true}

// watchers is the signum ADR-039 names: five things the node does with a
// watcher, each its own sigil.
func watchers() Signum {
	return Signum{
		Name: "watchers",
		Sigils: []Sigil{
			{Name: "list", Does: "List every watcher.", Answer: answers,
				HTTP: Endpoint{Method: http.MethodGet, Path: "/api/watchers"}},
			{Name: "create", Does: "Create a watcher.", Answer: answers,
				HTTP: Endpoint{Method: http.MethodPost, Path: "/api/watchers"}},
			{Name: "read", Does: "Read one watcher by its id.", Answer: answers, Takes: []Param{which},
				HTTP: Endpoint{Method: http.MethodGet, Path: "/api/watchers/{id}"}},
			{Name: "update", Does: "Update one watcher by its id.", Answer: answers, Takes: []Param{which},
				HTTP: Endpoint{Method: http.MethodPut, Path: "/api/watchers/{id}"}},
			{Name: "delete", Does: "Delete one watcher by its id.", Answer: answers, Takes: []Param{which},
				HTTP: Endpoint{Method: http.MethodDelete, Path: "/api/watchers/{id}"}},
		},
	}
}

func TestASignumThatSaysWhatItHoldsIsKept(t *testing.T) {
	require.NoError(t, watchers().Check())
}

// A sigil is the one place its thing is defined. One that leaves a part out
// has defined nothing, and the refusal says which sigil and which part.
func TestASigilThatLeavesAPartOutIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Signum)
		says   string
	}{
		{"a signum with no name", func(s *Signum) { s.Name = "" }, "a signum has no name"},
		{"a signum holding nothing", func(s *Signum) { s.Sigils = nil }, "the signum watchers holds no sigils"},
		{"a sigil with no name", func(s *Signum) { s.Sigils[0].Name = "" }, "a sigil of watchers has no name (GET /api/watchers)"},
		{"a sigil that does not say what it does", func(s *Signum) { s.Sigils[1].Does = "" }, "the sigil create of watchers does not say what it does"},
		{"nothing that answers", func(s *Signum) { s.Sigils[4].Answer = nil }, "the sigil delete of watchers has nothing that answers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signum := watchers()
			tc.change(&signum)
			require.EqualError(t, signum.Check(), tc.says)
		})
	}
}

// The HTTP API is a surface of a sigil. Its endpoint is said beside the sigil,
// so the sigil still reads in one place, and it is not what the sigil is.
func TestAnEndpointThatCannotBeCalledIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Signum)
		says   string
	}{
		{"a method that is not one", func(s *Signum) { s.Sigils[2].HTTP.Method = "FETCH" }, `the sigil read of watchers is bound to the method "FETCH", which is not one`},
		{"a path that does not start at /", func(s *Signum) { s.Sigils[3].HTTP.Path = "api/watchers/{id}" }, `the sigil update of watchers is bound to the path "api/watchers/{id}", which does not start at /`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signum := watchers()
			tc.change(&signum)
			require.EqualError(t, signum.Check(), tc.says)
		})
	}
}

// A sigil says what goes in, so a caller reads what to send rather than
// guessing it from a refusal. A param that does not say what it is tells the
// caller nothing, and a path that names a segment nothing fills cannot be
// called.
func TestASigilSaysWhatGoesIn(t *testing.T) {
	limit := Param{Name: "limit", Says: "How many watchers at most."}
	for _, tc := range []struct {
		name   string
		change func(*Signum)
		says   string
	}{
		{"a param with no name", func(s *Signum) { s.Sigils[0].Takes = []Param{{Says: "How many."}} }, "the sigil list of watchers takes a param with no name"},
		{"a param that does not say what it is", func(s *Signum) { s.Sigils[0].Takes = []Param{{Name: "limit"}} }, "the sigil list of watchers takes limit and does not say what it is"},
		{"a param taken twice", func(s *Signum) { s.Sigils[0].Takes = []Param{limit, limit} }, "the sigil list of watchers takes limit twice"},
		{"a segment nothing fills", func(s *Signum) { s.Sigils[2].Takes = nil }, "the sigil read of watchers is bound to /api/watchers/{id} and takes no id"},
		{"a segment filled by something optional", func(s *Signum) { s.Sigils[2].Takes = []Param{{Name: "id", Says: "Which watcher."}} }, "the sigil read of watchers is bound to /api/watchers/{id}, so id is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signum := watchers()
			tc.change(&signum)
			require.EqualError(t, signum.Check(), tc.says)
		})
	}

	kept := watchers()
	kept.Sigils[0].Takes = []Param{limit}
	require.NoError(t, kept.Check())
}

// A sigil refuses in its own terms, and the refusal names the param the caller
// has to change. A connector was refused five times by a breakdown that listed
// the values it takes and never said they belong to type.
func TestARefusalNamesTheParam(t *testing.T) {
	breakdown := Sigil{
		Name: "metrics",
		Takes: []Param{
			{Name: "market", Says: "The market the stand is in.", Required: true},
			{Name: "type", Says: "What to group the arrivals by.", Required: true, OneOf: []string{"page", "event", "referrer"}},
			{Name: "limit", Says: "How many rows at most."},
		},
	}

	require.Nil(t, breakdown.Refuses(map[string]string{"market": "clean", "type": "referrer"}))

	missing := breakdown.Refuses(map[string]string{"market": "clean"})
	require.NotNil(t, missing)
	require.Equal(t, Missing, missing.Why)
	require.Equal(t, "type", missing.Param)
	require.Equal(t, "metrics needs type: What to group the arrivals by. One of page, event, referrer.", missing.Says)

	empty := breakdown.Refuses(map[string]string{"market": "", "type": "page"})
	require.NotNil(t, empty)
	require.Equal(t, "market", empty.Param)
	require.Equal(t, "metrics needs market: The market the stand is in.", empty.Says)

	wrong := breakdown.Refuses(map[string]string{"market": "clean", "type": "eyecolour"})
	require.NotNil(t, wrong)
	require.Equal(t, NotOneOf, wrong.Why)
	require.Equal(t, "type", wrong.Param)
	require.Equal(t, "metrics takes type as one of page, event, referrer, and eyecolour is not one.", wrong.Says)
}

// A sigil says what comes out, by field. Said beside code that writes the
// answer it is a second place, so an answer can be held to it: a field the
// sigil promised and the answer lacks, or one the answer carries and the sigil
// never named, is said by name.
func TestAnAnswerIsHeldToWhatTheSigilGives(t *testing.T) {
	breakdown := Sigil{
		Name: "metrics",
		Gives: []Field{
			{Name: "type", Says: "What the arrivals were grouped by."},
			{Name: "counts", Says: "One row per value, most first: its name and its count."},
		},
	}

	require.NoError(t, breakdown.Holds([]byte(`{"type":"page","counts":[{"name":"/","count":2}]}`)))
	require.EqualError(t, breakdown.Holds([]byte(`{"type":"page"}`)),
		"metrics gives counts, and the answer has none")
	require.EqualError(t, breakdown.Holds([]byte(`{"type":"page","counts":[],"took_ms":3}`)),
		"the answer carries took_ms, which metrics never said it gives")
	require.EqualError(t, breakdown.Holds([]byte(`not json`)),
		"metrics gives fields, and the answer is not JSON")

	// Rows are held one at a time: what a list gives is what each row carries.
	rows := Sigil{Name: "list", Gives: []Field{{Name: "id", Says: "Which watcher."}}}
	require.NoError(t, rows.Holds([]byte(`[{"id":"a"},{"id":"b"}]`)))
	require.EqualError(t, rows.Holds([]byte(`[{"id":"a"},{"name":"b"}]`)),
		"list gives id, and the answer has none")
}

func TestAFieldThatLeavesAPartOutIsRefused(t *testing.T) {
	id := Field{Name: "id", Says: "Which watcher."}
	for _, tc := range []struct {
		name  string
		gives []Field
		says  string
	}{
		{"a field with no name", []Field{{Says: "Which watcher."}}, "the sigil list of watchers gives a field with no name"},
		{"a field that does not say what it is", []Field{{Name: "id"}}, "the sigil list of watchers gives id and does not say what it is"},
		{"a field given twice", []Field{id, id}, "the sigil list of watchers gives id twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signum := watchers()
			signum.Sigils[0].Gives = tc.gives
			require.EqualError(t, signum.Check(), tc.says)
		})
	}
}

// A signum crosses to the browser and to a plugin as proto (ADR-006). All of
// it crosses but the function that answers, which is not a shape.
func TestASignumCrossesWhole(t *testing.T) {
	signum := watchers()
	signum.Sigils[2].Takes = append(signum.Sigils[2].Takes,
		Param{Name: "as", Says: "The form to read it in.", OneOf: []string{"row", "full"}},
		Param{Name: "depth", Says: "How far down.", Kind: Count})
	signum.Sigils[2].Gives = []Field{{Name: "id", Says: "Which watcher."}}

	crossed := signum.Proto()
	require.Equal(t, "watchers", crossed.GetName())
	require.Len(t, crossed.GetSigils(), 5)

	read := crossed.GetSigils()[2]
	require.Equal(t, "read", read.GetName())
	require.Equal(t, "Read one watcher by its id.", read.GetDoes())
	require.Equal(t, http.MethodGet, read.GetHttp().GetMethod())
	require.Equal(t, "/api/watchers/{id}", read.GetHttp().GetPath())

	require.Len(t, read.GetTakes(), 3)
	require.Empty(t, read.GetTakes()[1].GetKind(), "text names no kind")
	require.Equal(t, "count", read.GetTakes()[2].GetKind())
	require.Equal(t, "id", read.GetTakes()[0].GetName())
	require.True(t, read.GetTakes()[0].GetRequired())
	require.Equal(t, "as", read.GetTakes()[1].GetName())
	require.Equal(t, "The form to read it in.", read.GetTakes()[1].GetSays())
	require.False(t, read.GetTakes()[1].GetRequired())
	require.Equal(t, []string{"row", "full"}, read.GetTakes()[1].GetOneOf())

	require.Len(t, read.GetGives(), 1)
	require.Equal(t, "id", read.GetGives()[0].GetName())
	require.Equal(t, "Which watcher.", read.GetGives()[0].GetSays())

	refused := Refusal{Why: NotOneOf, Param: "as", Says: "read takes as as one of row, full, and wide is not one."}.Proto()
	require.Equal(t, "not one of", refused.GetWhy())
	require.Equal(t, "as", refused.GetParam())
	require.Equal(t, "read takes as as one of row, full, and wide is not one.", refused.GetSays())
}

// What arrives is read the same way on every surface: a query's strings, a
// body's fields and a tool's arguments all become what was sent, by the names
// the sigil takes and as the kind each one is. What the sigil does not take
// never reaches the function that answers.
func TestWhatArrivesIsReadAsWhatTheSigilTakes(t *testing.T) {
	breakdown := Sigil{
		Name: "metrics",
		Takes: []Param{
			{Name: "market", Says: "The market.", Required: true},
			{Name: "limit", Says: "How many rows at most.", Kind: Count},
		},
	}

	sent, refusal := breakdown.Read(map[string]any{"market": "clean", "limit": float64(5), "colour": "blue"})
	require.Nil(t, refusal)
	require.Equal(t, Sent{"market": "clean", "limit": "5"}, sent, "a tool's number and a query's string are one thing, and colour is not taken")

	sent, refusal = breakdown.Read(map[string]any{"market": "clean", "limit": "5"})
	require.Nil(t, refusal)
	require.Equal(t, Sent{"market": "clean", "limit": "5"}, sent)

	for _, tc := range []struct {
		name    string
		arrived map[string]any
		param   string
		says    string
	}{
		{"text that is a list", map[string]any{"market": []any{"clean"}}, "market", "metrics takes market as text, and what was sent is not."},
		{"a count that is a fraction", map[string]any{"market": "clean", "limit": 2.5}, "limit", "metrics takes limit as a count, and 2.5 is not one."},
		{"a count that is a word", map[string]any{"market": "clean", "limit": "many"}, "limit", "metrics takes limit as a count, and many is not one."},
		{"a count below zero", map[string]any{"market": "clean", "limit": "-1"}, "limit", "metrics takes limit as a count, and -1 is not one."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, refusal := breakdown.Read(tc.arrived)
			require.NotNil(t, refusal)
			require.Equal(t, Invalid, refusal.Why)
			require.Equal(t, tc.param, refusal.Param)
			require.Equal(t, tc.says, refusal.Says)
		})
	}
}

// Every surface refuses the same way. The HTTP API's form of a refusal is a
// status, and what was wrong with what was sent is the caller's to fix.
func TestTheHTTPAPIGivesARefusalAStatus(t *testing.T) {
	for why, status := range map[Why]int{
		Missing:    http.StatusBadRequest,
		NotOneOf:   http.StatusBadRequest,
		Invalid:    http.StatusBadRequest,
		NotFound:   http.StatusNotFound,
		NotAllowed: http.StatusForbidden,
		Failed:     http.StatusInternalServerError,
	} {
		require.Equal(t, status, Refusal{Why: why}.Status(), string(why))
	}
}

// The function that answers is no surface's: it is handed what was sent and
// gives back the answer or a refusal, and never sees a request or a response.
func TestAnAnswerIsNoSurfaces(t *testing.T) {
	greets := Sigil{Name: "greet", Answer: func(_ context.Context, sent Sent) (any, *Refusal) {
		if sent["name"] == "nobody" {
			return nil, &Refusal{Why: NotFound, Param: "name", Says: "greet knows no nobody."}
		}
		return map[string]any{"greeting": "hello " + sent["name"]}, nil
	}}

	answer, refusal := greets.Answer(context.Background(), Sent{"name": "tim"})
	require.Nil(t, refusal)
	require.Equal(t, map[string]any{"greeting": "hello tim"}, answer)

	_, refusal = greets.Answer(context.Background(), Sent{"name": "nobody"})
	require.NotNil(t, refusal)
	require.Equal(t, http.StatusNotFound, refusal.Status())
}

// "the tool is equivalent to 1 sigil each"
//
// A name is one tool and an endpoint is one sigil, so neither is held twice.
func TestOneThingIsDefinedOnce(t *testing.T) {
	twiceNamed := watchers()
	twiceNamed.Sigils[1].Name = "list"
	require.EqualError(t, twiceNamed.Check(), "watchers holds the sigil list twice")

	twiceBound := watchers()
	twiceBound.Sigils[2].HTTP.Method = http.MethodPut
	require.EqualError(t, twiceBound.Check(), "read and update of watchers are both bound to PUT /api/watchers/{id}")
}
