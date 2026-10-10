package grpc

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// Traffic is what the node carried to one plugin over HTTP since this process
// started: per route the plugin declared, how often it was called and how it
// answered. The node proxies every call, so it counts them; a plugin is never
// asked to count itself.
type Traffic struct {
	mu     sync.Mutex
	since  time.Time
	routes map[string]*RouteTraffic
}

// RouteTraffic is one route's count.
type RouteTraffic struct {
	// Route is "METHOD /path" as the plugin declared it, or Undeclared.
	Route   string        `json:"route"`
	Calls   int64         `json:"calls"`
	Refused int64         `json:"4xx"`
	Broke   int64         `json:"5xx"`
	Took    time.Duration `json:"took_ns"`
	Last    time.Time     `json:"last"`
}

// Undeclared is every path the plugin never declared, counted together: a raw
// path per key would let whoever calls grow the table without end.
const Undeclared = "undeclared"

func newTraffic() *Traffic {
	return &Traffic{since: time.Now(), routes: map[string]*RouteTraffic{}}
}

// routeOf names a call by the route it matches, so ids in a path do not
// become rows of their own.
func routeOf(method, path string, declared []*protocol.RouteInfo) string {
	// Both sides lose a trailing slash, so "/" is "" on both and matches.
	path = strings.TrimSuffix(path, "/")
	for _, r := range declared {
		if r.GetMethod() == method && strings.TrimSuffix(r.GetPath(), "/") == path {
			return method + " " + r.GetPath()
		}
	}
	return Undeclared
}

// outcomeOf is a status in a word, the closed set AttrOutcome carries.
// The three ranges are every status there is.
func outcomeOf(status int) string {
	if status >= 500 {
		return "5xx"
	}
	if status >= 400 {
		return "4xx"
	}
	return "ok"
}

func (t *Traffic) record(route string, status int, took time.Duration, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.routes[route]
	if !ok {
		r = &RouteTraffic{Route: route}
		t.routes[route] = r
	}
	r.Calls++
	switch {
	case status >= 500:
		r.Broke++
	case status >= 400:
		r.Refused++
	}
	r.Took += took
	r.Last = at
}

// Since is when counting started: this process, not the plugin's.
func (t *Traffic) Since() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.since
}

// Routes is every route called so far, the most called first.
func (t *Traffic) Routes() []RouteTraffic {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]RouteTraffic, 0, len(t.routes))
	for _, r := range t.routes {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(a, b RouteTraffic) int {
		if a.Calls != b.Calls {
			if a.Calls > b.Calls {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Route, b.Route)
	})
	return out
}
