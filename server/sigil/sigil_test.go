package sigil

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func answers(http.ResponseWriter, *http.Request) {}

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
			{Name: "read", Does: "Read one watcher by its id.", Answer: answers,
				HTTP: Endpoint{Method: http.MethodGet, Path: "/api/watchers/{id}"}},
			{Name: "update", Does: "Update one watcher by its id.", Answer: answers,
				HTTP: Endpoint{Method: http.MethodPut, Path: "/api/watchers/{id}"}},
			{Name: "delete", Does: "Delete one watcher by its id.", Answer: answers,
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
