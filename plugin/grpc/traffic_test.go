package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

var declared = []*protocol.RouteInfo{
	{Method: "POST", Path: "/me/draft"},
	{Method: "GET", Path: "/health"},
}

func TestACallIsNamedByTheRouteItMatches(t *testing.T) {
	assert.Equal(t, "POST /me/draft", routeOf("POST", "/me/draft", declared))
	assert.Equal(t, "POST /me/draft", routeOf("POST", "/me/draft/", declared))
	assert.Equal(t, "GET /health", routeOf("GET", "/health", declared))
}

// A path nobody declared, or a declared path under another method, is one row
// together however many different paths arrive.
func TestWhatWasNeverDeclaredIsCountedTogether(t *testing.T) {
	assert.Equal(t, Undeclared, routeOf("GET", "/me/draft", declared))
	assert.Equal(t, Undeclared, routeOf("GET", "/anything/123", declared))
}

func TestARouteCountsItsCallsAndHowItAnswered(t *testing.T) {
	tr := newTraffic()
	now := time.Now()
	tr.record("POST /me/draft", 200, time.Millisecond, now)
	tr.record("POST /me/draft", 400, time.Millisecond, now)
	tr.record("POST /me/draft", 503, time.Millisecond, now)
	tr.record("GET /health", 200, time.Millisecond, now)

	routes := tr.Routes()
	assert.Equal(t, "POST /me/draft", routes[0].Route, "the most called comes first")
	assert.Equal(t, int64(3), routes[0].Calls)
	assert.Equal(t, int64(1), routes[0].Refused)
	assert.Equal(t, int64(1), routes[0].Broke)
	assert.Equal(t, 3*time.Millisecond, routes[0].Took)
	assert.Equal(t, int64(1), routes[1].Calls)
}
