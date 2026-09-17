package sigil

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func answers(http.ResponseWriter, *http.Request) {}

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

// The HTTP API is a binding of a sigil. Its endpoint is said beside the sigil,
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

// Every binding refuses the same way. The HTTP API's form of a refusal is a
// status, and what was wrong with what was sent is the caller's to fix.
func TestTheHTTPAPIGivesARefusalAStatus(t *testing.T) {
	require.Equal(t, http.StatusBadRequest, Refusal{Why: Missing}.Status())
	require.Equal(t, http.StatusBadRequest, Refusal{Why: NotOneOf}.Status())
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
