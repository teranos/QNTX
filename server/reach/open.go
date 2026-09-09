package reach

import (
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// Answering is a handler offered by the path it would answer on.

// Socket says the handler is a WebSocket upgrade, which is served through a
// different rate limiter and is otherwise the same question.
type Answering struct {
	Handler http.HandlerFunc
	Socket  bool
}

// Gate wraps a handler so that only the levels a line granted go through. The
// server supplies it because the middleware belongs to auth, not here. path
// is the reach table's own pattern for the route being wrapped, handed
// through so a refusal can be counted by which route refused it.
type Gate func(path string, reach auth.Reach, handler http.HandlerFunc) http.HandlerFunc

// Wrapping is the rest of what a request passes on the way in — logging, CORS,
// the rate limiters. Open asks for them rather than reaching for them, because
// this package holds the mux and nothing else about how the node is built.
type Wrapping struct {
	Gate Gate
	// Anyone is the chain for a route served without asking who is calling.
	Anyone func(http.HandlerFunc) http.HandlerFunc
	// Asked is the chain for a route that is gated.
	Asked func(http.HandlerFunc) http.HandlerFunc
	// Upgraded is the chain for a gated WebSocket upgrade.
	Upgraded func(http.HandlerFunc) http.HandlerFunc
}

// Served is what the node serves. It is an http.Handler and nothing else: a
// caller holding one cannot add a route to it.

// A plugin enabled after startup arrives by Reopen, which asks the table again
// for everything. There is no call that adds one route.
type Served struct {
	holding atomic.Pointer[http.ServeMux]
	// granters is who may grant each role, read off the lines with the mux:
	// what came after `by`. ROOT is never listed; ROOT grants everything.
	granters atomic.Pointer[map[string][]string]
}

// Granters is who may grant a role besides ROOT: the levels and roles the
// role's reach lines named after `by`. A role no line names after `by` is
// ROOT's alone to grant. This is the whole of phase 4 of #899 on the reach
// side; the write gate asks it.
func (s *Served) Granters(role string) []string {
	held := s.granters.Load()
	if held == nil {
		return nil
	}
	return slices.Clone((*held)[strings.ToUpper(role)])
}

func (s *Served) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mux := s.holding.Load()
	if mux == nil {
		// Nothing was opened, so nothing is served. Not an empty mux that
		// would 404 as though the node were up and answering.
		http.Error(w, "the node is not serving", http.StatusServiceUnavailable)
		return
	}
	mux.ServeHTTP(w, r)
}

// Open builds what the node serves out of the table and the store's lines. The
// second return is the handlers this build carries that no line names: ROOT's
// and nobody else's.
func Open(answering map[string]Answering, with Wrapping, runtime Runtime) (*Served, []string, error) {
	served := &Served{}
	unnamed, err := served.Reopen(answering, with, runtime)
	if err != nil {
		return nil, nil, err
	}
	return served, unnamed, nil
}

// Reopen asks the table again and replaces what is served, whole. Plugins come
// and go by editing am.toml, and a plugin's routes are granted or they are not.
// A reach line written at runtime arrives the same way: the store is read
// again and the mux is rebuilt, never patched.
func (s *Served) Reopen(answering map[string]Answering, with Wrapping, runtime Runtime) ([]string, error) {
	granted, err := readReaches(reachTable)
	if err != nil {
		return nil, err
	}
	granters := addRuntime(granted, runtime)
	mux, unnamed, err := build(granted, answering, with)
	if err != nil {
		return nil, err
	}
	s.holding.Store(mux)
	s.granters.Store(&granters)
	return unnamed, nil
}

// build is the whole of it, against a table the caller supplies — which is how
// the tests reach it. The production table is not a parameter anywhere.
func build(granted map[string]aRow, answering map[string]Answering, with Wrapping) (*http.ServeMux, []string, error) {
	mux := http.NewServeMux()

	for _, path := range sorted(granted) {
		answers, ok := answering[path]
		if !ok {
			return nil, nil, errors.Newf("a line grants reach to %s, and nothing answers there", path)
		}
		serve(mux, path, granted[path], answers, with)
	}

	// Handlers this build carries that no line names. Root gets everything and
	// everyone else does not: an empty row admits ROOT and nobody else, which
	// is what not being defined means.
	var unnamed []string
	for _, path := range sorted(answering) {
		if _, said := granted[path]; said {
			continue
		}
		serve(mux, path, aRow{}, answering[path], with)
		unnamed = append(unnamed, path)
	}
	return mux, unnamed, nil
}

// serve puts one route on the mux behind what its row admits.
func serve(mux *http.ServeMux, path string, row aRow, answers Answering, with Wrapping) {
	switch {
	case row.anyone:
		mux.HandleFunc(path, with.Anyone(answers.Handler))
	case answers.Socket:
		mux.HandleFunc(path, with.Upgraded(with.Gate(path, row.reach, answers.Handler)))
	default:
		mux.HandleFunc(path, with.Asked(with.Gate(path, row.reach, answers.Handler)))
	}
}

func sorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
