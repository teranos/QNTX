package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/server/auth"
)

// The log line is what says who a request turned out to be. Middleware hands
// the caller down on a copy of the request, so reading the outer one found
// nothing and every line the branch added to make refusals readable was blank.
func TestAccessLogSeesTheCaller(t *testing.T) {
	ctx, seen := auth.WithCallerSink(httptest.NewRequest(http.MethodGet, "/api/x", nil).Context())

	inner := func(_ http.ResponseWriter, r *http.Request) {
		auth.WithCaller(r.Context(), auth.Caller{
			Level:     auth.LevelSuper,
			Identity:  "https://mastodon.example/@tim",
			Namespace: auth.NamespaceDefault,
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/x", nil).WithContext(ctx)
	inner(httptest.NewRecorder(), req)

	if seen.Identity != "https://mastodon.example/@tim" {
		t.Fatalf("the sink did not learn the identity: %q", seen.Identity)
	}
	if seen.Level != auth.LevelSuper {
		t.Fatalf("the sink did not learn the level: %q", seen.Level)
	}
}

// A request that never reaches auth has no caller, and the sink says so by
// staying empty rather than by inventing one.
func TestAccessLogSinkIsEmptyWithoutAuth(t *testing.T) {
	_, seen := auth.WithCallerSink(httptest.NewRequest(http.MethodGet, "/health", nil).Context())

	if seen.Identity != "" || seen.Level != "" {
		t.Fatalf("an unauthenticated request produced identity=%q level=%q", seen.Identity, seen.Level)
	}
}
