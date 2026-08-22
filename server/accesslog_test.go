package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// A poll that answers 303 every second is still a poll. Pinning the quiet rule
// to 200 meant /statusline wrote a line a second and buried everything else.
func TestAHeartbeatIsQuietWhateverItAnswers(t *testing.T) {
	if !heartbeat("/statusline", http.StatusSeeOther, time.Millisecond) {
		t.Fatal("a fast 303 on /statusline should be quiet")
	}
	if !heartbeat("/api/version", http.StatusOK, time.Millisecond) {
		t.Fatal("a fast 200 on /api/version should be quiet")
	}
}

// Thinned, not silenced. The first poll speaks so the log says it is happening,
// and then it says so once a minute rather than once a second.
func TestAHeartbeatSpeaksOnceThenHolds(t *testing.T) {
	var beats heartbeats
	at := time.Unix(1787424523, 0)

	if !beats.worthSaying("/statusline", at) {
		t.Fatal("the first poll should get a line")
	}
	for i := 1; i < 60; i++ {
		if beats.worthSaying("/statusline", at.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("second %d should have been held", i)
		}
	}
	if !beats.worthSaying("/statusline", at.Add(heartbeatEvery)) {
		t.Fatal("a minute on, it should speak again")
	}
}

// One path going quiet does not quiet another.
func TestHeartbeatsAreHeldPerPath(t *testing.T) {
	var beats heartbeats
	at := time.Unix(1787424523, 0)

	if !beats.worthSaying("/statusline", at) {
		t.Fatal("the first /statusline should speak")
	}
	if !beats.worthSaying("/api/version", at) {
		t.Fatal("a different path has its own first time")
	}
}

// It goes quiet when it is dull, and speaks up when it refuses, fails or drags.
func TestAHeartbeatStillSpeaksWhenItMatters(t *testing.T) {
	if heartbeat("/api/plugins", http.StatusUnauthorized, time.Millisecond) {
		t.Fatal("a refusal is worth a line")
	}
	if heartbeat("/statusline", http.StatusInternalServerError, time.Millisecond) {
		t.Fatal("a failure is worth a line")
	}
	if heartbeat("/statusline", http.StatusSeeOther, time.Second) {
		t.Fatal("a slow answer is worth a line")
	}
	if heartbeat("/auth/user/arrive", http.StatusOK, time.Millisecond) {
		t.Fatal("a path nobody polls is always worth a line")
	}
}
