package server

import (
	"fmt"
	"github.com/teranos/QNTX/plugin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

	admitted := auth.Admitted(auth.LevelSuper, auth.NamespaceDefault)
	admitted.Identity = "https://mastodon.example/@tim"

	inner := func(_ http.ResponseWriter, r *http.Request) {
		auth.WithAdmission(r.Context(), admitted)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/x", nil).WithContext(ctx)
	inner(httptest.NewRecorder(), req)

	if seen.Identity != "https://mastodon.example/@tim" {
		t.Fatalf("the sink did not learn the identity: %q", seen.Identity)
	}
	if seen.LevelName() != string(auth.LevelSuper) {
		t.Fatalf("the sink did not learn the level: %q", seen.LevelName())
	}
}

// The sink learns who, and the line says only what level they were. An account
// id on every request reaches every sink the logger has, forever, for nothing.
func TestTheAccessLogNeverCarriesTheIdentity(t *testing.T) {
	core, written := observer.New(zapcore.InfoLevel)
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.New(core).Sugar()}

	admitted := auth.Admitted(auth.LevelSuper)
	admitted.Identity = "google:110106507016968762213"

	// A refusal, because that is the line that gets written, and the one where
	// naming the refused would be the most tempting.
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) {
		auth.WithAdmission(r.Context(), admitted)
		w.WriteHeader(http.StatusForbidden)
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

	if seen.Identity != "" || seen.LevelName() != "" {
		t.Fatalf("an unauthenticated request produced identity=%q level=%q", seen.Identity, seen.LevelName())
	}
}

// A poll that answers 303 every second went well. Pinning this to 200 meant
// /am/statusline wrote a line a second and buried everything else.
func TestAPollWentWellWhateverItAnswers(t *testing.T) {
	if !heartbeatWell(http.StatusSeeOther) {
		t.Fatal("a 303 went well")
	}
	if !heartbeatWell(http.StatusOK) {
		t.Fatal("a 200 went well")
	}
	if heartbeatWell(http.StatusUnauthorized) {
		t.Fatal("a refusal did not go well")
	}
	if heartbeatWell(http.StatusInternalServerError) {
		t.Fatal("a failure did not go well")
	}
}

// A successful poll carries nothing: the answer was known before it was asked.
// So it says itself twice, and then says nothing however long it runs.
func TestASucceedingPollGoesQuietAndStaysQuiet(t *testing.T) {
	var beats heartbeats

	for i := range heartbeatSays {
		if !beats.worthSaying("/am/statusline", true) {
			t.Fatalf("poll %d should have spoken", i+1)
		}
	}
	for i := range 10000 {
		if beats.worthSaying("/am/statusline", true) {
			t.Fatalf("poll %d spoke while nothing had changed", i+heartbeatSays+1)
		}
	}
}

// A poll failing for an hour is one fact, not three thousand. Whatever the
// state, it says itself twice and then holds.
func TestAFailingPollAlsoGoesQuiet(t *testing.T) {
	var beats heartbeats

	for i := range heartbeatSays {
		if !beats.worthSaying("/am/statusline", false) {
			t.Fatalf("failure %d should have spoken", i+1)
		}
	}
	for i := range 10000 {
		if beats.worthSaying("/am/statusline", false) {
			t.Fatalf("failure %d spoke while nothing had changed", i+heartbeatSays+1)
		}
	}
}

// The turn is the news. Breaking speaks, and so does coming back.
func TestATurnAlwaysSpeaks(t *testing.T) {
	var beats heartbeats

	for range 100 {
		beats.worthSaying("/am/statusline", true)
	}
	if !beats.worthSaying("/am/statusline", false) {
		t.Fatal("going from well to failing is worth a line")
	}
	for range 100 {
		beats.worthSaying("/am/statusline", false)
	}
	if !beats.worthSaying("/am/statusline", true) {
		t.Fatal("coming back is worth a line")
	}
}

// One path going quiet does not quiet another.
func TestHeartbeatsAreHeldPerPath(t *testing.T) {
	var beats heartbeats

	for range heartbeatSays {
		beats.worthSaying("/am/statusline", true)
	}
	if beats.worthSaying("/am/statusline", true) {
		t.Fatal("/am/statusline should have gone quiet")
	}
	if !beats.worthSaying("/am/version", true) {
		t.Fatal("a different path has its own count")
	}
}

// A path nobody polls is not thinned at all — the caller checks the list, and
// everything outside it says every request.
func TestOnlyPolledPathsAreThinned(t *testing.T) {
	if !heartbeatPaths["/am/statusline"] {
		t.Fatal("/am/statusline is polled")
	}
	if heartbeatPaths["/auth/user/arrive"] {
		t.Fatal("arriving is not a poll, and is always worth a line")
	}
}

// A ground asks for a comet nobody has built, every few minutes, and is told the
// same each time. That is a state: said twice, and again only when the answer
// changes. A POST that builds one is never a poll.
func TestAGroundAskingForAnUnbuiltCometIsSaidTwiceAndThenQuiet(t *testing.T) {
	core, written := observer.New(zapcore.InfoLevel)
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.New(core).Sugar()}
	status := http.StatusNotFound
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })

	ask := func(method, path string) {
		handler(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
	}
	for range 6 {
		ask(http.MethodGet, "/comet/teranos/QNTX/aarch64-darwin/1")
	}
	if got := written.Len(); got != 2 {
		t.Fatalf("six identical refusals wrote %d lines, want 2", got)
	}

	ask(http.MethodGet, "/comet/teranos/QNTX/aarch64-darwin/2")
	if got := written.Len(); got != 3 {
		t.Fatalf("another comet's refusal wrote %d lines in all, want 3", got)
	}

	for range 3 {
		ask(http.MethodPost, "/comet/teranos/QNTX/aarch64-darwin")
	}
	if got := written.Len(); got != 6 {
		t.Fatalf("three refused builds wrote %d lines in all, want 6", got)
	}

	// The same path refused for who is asking is not a ground waiting on a
	// build: it is said every time.
	status = http.StatusForbidden
	for range 3 {
		ask(http.MethodGet, "/comet/teranos/QNTX/aarch64-darwin/1")
	}
	if got := written.Len(); got != 9 {
		t.Fatalf("three 403s on a comet path wrote %d lines in all, want 9", got)
	}
}

// A caller who writes a new comet path every time cannot make the node
// remember each one. Past what is held, a new path is said every time, and the
// ones already held stay quiet.
func TestTheSetOfPollsTheAccessLogHoldsDoesNotGrowWithoutEnd(t *testing.T) {
	var beats heartbeats
	for i := range heartbeatHolds {
		beats.worthSaying(fmt.Sprintf("/comet/o/r/p/%d", i), false)
	}
	for range 3 {
		if !beats.worthSaying("/comet/o/r/p/new", false) {
			t.Fatal("a path past the held set was not said")
		}
	}
	if beats.held != heartbeatHolds {
		t.Fatalf("held %d paths, want %d", beats.held, heartbeatHolds)
	}
	beats.worthSaying("/comet/o/r/p/0", false)
	if beats.worthSaying("/comet/o/r/p/0", false) {
		t.Fatal("a held path that was said twice spoke a third time")
	}
}
