package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// The log line is what says who a request turned out to be. Middleware hands
// the caller down on a copy of the request, so reading the outer one found
// nothing and every line the branch added to make refusals readable was blank.
func TestAccessLogSeesTheAdmission(t *testing.T) {
	ctx, seen := auth.WithAdmissionSink(httptest.NewRequest(http.MethodGet, "/api/x", nil).Context())

	inner := func(_ http.ResponseWriter, r *http.Request) {
		auth.WithAdmission(r.Context(), auth.Admission{
			Level:      auth.LevelSuper,
			Identity:   "https://mastodon.example/@tim",
			Namespaces: []string{auth.NamespaceDefault},
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

// The sink learns who, and the line says only what level they were. An account
// id on every request reaches every sink the logger has, forever, for nothing.
func TestTheAccessLogNeverCarriesTheIdentity(t *testing.T) {
	core, written := observer.New(zapcore.InfoLevel)
	s := &QNTXServer{logger: zap.New(core).Sugar()}

	handler := s.accessLog(func(_ http.ResponseWriter, r *http.Request) {
		auth.WithAdmission(r.Context(), auth.Admission{
			Level:    auth.LevelSuper,
			Identity: "google:110106507016968762213",
		})
	})
	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))

	if written.Len() == 0 {
		t.Fatal("the request wrote no line at all")
	}
	// The level has to be there, or this test would pass on a line it never read.
	if got := written.All()[0].ContextMap()["level"]; got != string(auth.LevelSuper) {
		t.Fatalf("level = %v, want %q — the line was not read", got, auth.LevelSuper)
	}
	for _, line := range written.All() {
		for key, value := range line.ContextMap() {
			if key == "identity" {
				t.Errorf("the line carries an identity field: %v", value)
			}
			if said, ok := value.(string); ok && strings.Contains(said, "110106507016968762213") {
				t.Errorf("the account id reached the log under %q: %q", key, said)
			}
		}
	}
}

// A request that never reaches auth has no caller, and the sink says so by
// staying empty rather than by inventing one.
func TestAccessLogSinkIsEmptyWithoutAuth(t *testing.T) {
	_, seen := auth.WithAdmissionSink(httptest.NewRequest(http.MethodGet, "/health", nil).Context())

	if seen.Identity != "" || seen.Level != "" {
		t.Fatalf("an unauthenticated request produced identity=%q level=%q", seen.Identity, seen.Level)
	}
}

// A poll that answers 303 every second went well. Pinning this to 200 meant
// /statusline wrote a line a second and buried everything else.
func TestAPollWentWellWhateverItAnswers(t *testing.T) {
	if !heartbeatWell(http.StatusSeeOther, time.Millisecond) {
		t.Fatal("a fast 303 went well")
	}
	if !heartbeatWell(http.StatusOK, time.Millisecond) {
		t.Fatal("a fast 200 went well")
	}
	if heartbeatWell(http.StatusUnauthorized, time.Millisecond) {
		t.Fatal("a refusal did not go well")
	}
	if heartbeatWell(http.StatusInternalServerError, time.Millisecond) {
		t.Fatal("a failure did not go well")
	}
	if heartbeatWell(http.StatusSeeOther, time.Second) {
		t.Fatal("a slow answer did not go well")
	}
}

// A successful poll carries nothing: the answer was known before it was asked.
// So it says itself twice, and then says nothing however long it runs.
func TestASucceedingPollGoesQuietAndStaysQuiet(t *testing.T) {
	var beats heartbeats

	for i := range heartbeatSays {
		if !beats.worthSaying("/statusline", true) {
			t.Fatalf("poll %d should have spoken", i+1)
		}
	}
	for i := range 10000 {
		if beats.worthSaying("/statusline", true) {
			t.Fatalf("poll %d spoke while nothing had changed", i+heartbeatSays+1)
		}
	}
}

// A poll failing for an hour is one fact, not three thousand. Whatever the
// state, it says itself twice and then holds.
func TestAFailingPollAlsoGoesQuiet(t *testing.T) {
	var beats heartbeats

	for i := range heartbeatSays {
		if !beats.worthSaying("/statusline", false) {
			t.Fatalf("failure %d should have spoken", i+1)
		}
	}
	for i := range 10000 {
		if beats.worthSaying("/statusline", false) {
			t.Fatalf("failure %d spoke while nothing had changed", i+heartbeatSays+1)
		}
	}
}

// The turn is the news. Breaking speaks, and so does coming back.
func TestATurnAlwaysSpeaks(t *testing.T) {
	var beats heartbeats

	for range 100 {
		beats.worthSaying("/statusline", true)
	}
	if !beats.worthSaying("/statusline", false) {
		t.Fatal("going from well to failing is worth a line")
	}
	for range 100 {
		beats.worthSaying("/statusline", false)
	}
	if !beats.worthSaying("/statusline", true) {
		t.Fatal("coming back is worth a line")
	}
}

// One path going quiet does not quiet another.
func TestHeartbeatsAreHeldPerPath(t *testing.T) {
	var beats heartbeats

	for range heartbeatSays {
		beats.worthSaying("/statusline", true)
	}
	if beats.worthSaying("/statusline", true) {
		t.Fatal("/statusline should have gone quiet")
	}
	if !beats.worthSaying("/api/version", true) {
		t.Fatal("a different path has its own count")
	}
}

// A path nobody polls is not thinned at all — the caller checks the list, and
// everything outside it says every request.
func TestOnlyPolledPathsAreThinned(t *testing.T) {
	if !heartbeatPaths["/statusline"] {
		t.Fatal("/statusline is polled")
	}
	if heartbeatPaths["/auth/user/arrive"] {
		t.Fatal("arriving is not a poll, and is always worth a line")
	}
}
