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
//
// Gates says the handler puts what it answers behind the gate itself. A path
// sigils are bound to holds sigils different people reach, and more lines than
// the path's own are about each one (ReachingSigil), so it is gated a sigil at
// a time by what answers there and the mux does not gate it again.
type Answering struct {
	Handler http.HandlerFunc
	Socket  bool
	Gates   bool
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
	// rows is what the lines said of each path, kept with the mux they built.
	rows atomic.Pointer[map[string]aRow]
	// routes is every path the mux was given, kept with it.
	routes atomic.Pointer[[]Route]
}

// A Route is one path the node serves, as it was offered: a WebSocket upgrade,
// or a path that gates itself because sigils answer there.
type Route struct {
	Path   string
	Socket bool
	Gates  bool
}

// Routes is every path the node serves, read off what it was opened with
// rather than off any document about it. Nothing before anything is open.
func (s *Served) Routes() []Route {
	held := s.routes.Load()
	if held == nil {
		return nil
	}
	return slices.Clone(*held)
}

// Reaching is what the lines say of one path: whether it is served without
// asking who is calling, and who reaches it otherwise.
//
// A tool call on a sigil is answered without a request of its own, so it is put
// behind the same gate with the row its path has here. A path no line names is
// ROOT's and nobody else's, which is the empty reach, and so is every path
// before anything is open.
func (s *Served) Reaching(path string) (reaching auth.Reach, anyone bool) {
	held := s.rows.Load()
	if held == nil {
		return auth.Reach{}, false
	}
	row := (*held)[path]
	return row.reach, row.anyone
}

// The surfaces a line can be about (ADR-039). A line that names none is about
// every surface.
const (
	OverHTTP = "http"
	OverMCP  = "mcp"
)

// ReachingSigil is who reaches one sigil over one surface. A line names what
// is reached by path or by name, and every line about the sigil counts:
//
//	'/api/staands/metrics'   the path it is bound to, which is the floor
//	'staands'                its signum, every sigil of it
//	'staands:metrics'        the sigil
//	'mcp:staands'            its signum over one surface
//	'mcp:staands:metrics'    the sigil over one surface
//
// Together they admit whoever any of them admits. A line only ever adds, which
// is what a line in the const and a line in the store both do already, so
// reaching a sigil over MCP and not over HTTP is a line that names MCP and no
// line that does not.
func (s *Served) ReachingSigil(surface, signum, sigil, path string) (reaching auth.Reach, anyone bool) {
	held := s.rows.Load()
	if held == nil {
		return auth.Reach{}, false
	}
	for _, named := range []string{
		path,
		signum,
		signum + ":" + sigil,
		surface + ":" + signum,
		surface + ":" + signum + ":" + sigil,
	} {
		row, said := (*held)[named]
		if !said {
			continue
		}
		reaching = reaching.With(row.reach)
		anyone = anyone || row.anyone
	}
	return reaching, anyone
}

// routesIn is the rows that are routes: the ones a path names. A row that
// names a sigil is about who reaches it and goes on no mux.
func routesIn(granted map[string]aRow) map[string]aRow {
	routes := map[string]aRow{}
	for named, row := range granted {
		if strings.HasPrefix(named, "/") {
			routes[named] = row
		}
	}
	return routes
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
	// Only a path is a route. A line that names a sigil says who reaches it and
	// is kept with the rest, for whatever answers without a route of its own.
	mux, unnamed, err := build(routesIn(granted), answering, with)
	if err != nil {
		return nil, err
	}
	// build serves every path it is offered, or none of them.
	routes := make([]Route, 0, len(answering))
	for _, path := range sorted(answering) {
		routes = append(routes, Route{Path: path, Socket: answering[path].Socket, Gates: answering[path].Gates})
	}
	s.holding.Store(mux)
	s.granters.Store(&granters)
	s.rows.Store(&granted)
	s.routes.Store(&routes)
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
	case answers.Gates:
		mux.HandleFunc(path, with.Asked(answers.Handler))
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
