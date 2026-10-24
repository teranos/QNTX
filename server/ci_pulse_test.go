package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/quote"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// A GitHub that answers from a script: the branch's history once, then the
// commit's runs for each ask until they conclude. Every ask is remembered.
type scriptedGitHub struct {
	history    string
	commits    []string
	dispatched []string
	// artifacts is a run's artifact listing, by run id. zips is what each
	// archive_download_url holds, served by blobs where locate points.
	artifacts map[string]string
	zips      map[string][]byte
	blobs     *httptest.Server
	located   []string
	asked     []string
	tokens    []string
}

const wholeSha = "abc123def4567890abc123def4567890abc123de"

func (g *scriptedGitHub) get(_ context.Context, token, url string) ([]byte, error) {
	g.asked = append(g.asked, url)
	g.tokens = append(g.tokens, token)
	if strings.Contains(url, "branch=") {
		return []byte(g.history), nil
	}
	if before, _, ok := strings.Cut(url, "/artifacts"); ok {
		_, run, _ := strings.Cut(before, "/actions/runs/")
		if listed, ok := g.artifacts[run]; ok {
			return []byte(listed), nil
		}
		return []byte(`{"total_count":0,"artifacts":[]}`), nil
	}
	// The commits endpoint answers a prefix with the whole sha.
	if strings.Contains(url, "/commits/abc123") {
		return []byte(`{"sha":"` + wholeSha + `"}`), nil
	}
	// The dispatched listing answers a name, not a sha.
	if strings.Contains(url, "event=workflow_dispatch") {
		if len(g.dispatched) == 0 {
			return []byte(`{"workflow_runs":[]}`), nil
		}
		next := g.dispatched[0]
		g.dispatched = g.dispatched[1:]
		return []byte(next), nil
	}
	if len(g.commits) == 0 {
		return []byte(`{"workflow_runs":[]}`), nil
	}
	next := g.commits[0]
	g.commits = g.commits[1:]
	return []byte(next), nil
}

// locate points an archive_download_url at blobs, as GitHub points it at a
// signed URL.
func (g *scriptedGitHub) locate(_ context.Context, token, archive string) (string, error) {
	g.located = append(g.located, archive)
	g.tokens = append(g.tokens, token)
	if _, ok := g.zips[archive]; !ok || g.blobs == nil {
		return "", errors.Newf("github answered 404 for %s", archive)
	}
	return g.blobs.URL + "/?zip=" + url.QueryEscape(archive), nil
}

// servingZips starts the blob store locate points at.
func servingZips(t *testing.T, g *scriptedGitHub) {
	t.Helper()
	g.blobs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("the token went to the signed URL")
		}
		zipped, ok := g.zips[r.URL.Query().Get("zip")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(zipped)
	}))
	t.Cleanup(g.blobs.Close)
}

func noHistory() string { return `{"workflow_runs":[]}` }

func runsJSON(runs ...string) string { return `{"workflow_runs":[` + strings.Join(runs, ",") + `]}` }

func aRun(name, status, conclusion, url string) string {
	return `{"name":"` + name + `","status":"` + status + `","conclusion":"` + conclusion + `","html_url":"` + url + `"}`
}

func ciStatusAs(actor string) *types.As {
	return &types.As{
		ID:         "ground:ci-status:sess-1",
		Subjects:   []string{"teranos/ground:sky-whisper"},
		Predicates: []string{watcher.CIPushedPredicate},
		Contexts:   []string{"session:sess-1"},
		Actors:     []string{actor, "ground"},
		Timestamp:  time.Now().Add(-10 * time.Second),
		Attributes: map[string]any{
			"repo":   "teranos/ground",
			"branch": "sky-whisper",
			"sha":    "abc123",
		},
	}
}

func jobFor(t *testing.T, as *types.As) *async.Job {
	t.Helper()
	payload, err := json.Marshal(as)
	if err != nil {
		t.Fatal(err)
	}
	return &async.Job{ID: "JB-test", HandlerName: watcher.CIWatchHandlerName, Payload: payload}
}

func handlerOver(gh *scriptedGitHub, news *statusline.NewsLog) *ciWatchHandler {
	return &ciWatchHandler{
		get:    gh.get,
		locate: gh.locate,
		token:  func(context.Context) (string, error) { return "tok-1", nil },
		sleep:  func(context.Context, time.Duration) error { return nil },
		news:   news,
		// The push was attested by alice's ground token; the news is hers.
		mintedBy: func(did string) (string, bool) {
			if did == "did:key:alice" {
				return "https://mastodon.example/@alice", true
			}
			return "", false
		},
		logger: zap.NewNop().Sugar(),
	}
}

// The run concludes; one item is left on the row, for the person whose token
// attested the push, under the attestation's own id and the commit.
func TestCIWatchLeavesNewsWhenTheRunConcludes(t *testing.T) {
	gh := &scriptedGitHub{
		history: runsJSON(
			`{"run_started_at":"2026-09-22T10:00:00Z","updated_at":"2026-09-22T10:00:40Z","status":"completed","conclusion":"success"}`,
			`{"run_started_at":"2026-09-22T11:00:00Z","updated_at":"2026-09-22T11:01:30Z","status":"completed","conclusion":"success"}`,
		),
		commits: []string{
			runsJSON(aRun("Go", "completed", "success", "https://github.com/teranos/ground/actions/runs/1"),
				aRun("Nix", "in_progress", "", "https://github.com/teranos/ground/actions/runs/2")),
			runsJSON(aRun("Go", "completed", "success", "https://github.com/teranos/ground/actions/runs/1"),
				aRun("Nix", "completed", "success", "https://github.com/teranos/ground/actions/runs/2")),
		},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)

	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 {
		t.Fatalf("news for alice: %d items, want 1: %+v", len(got), got)
	}
	if left := news.Since("did:key:alice", time.Now().UnixMilli()); len(left) != 0 {
		t.Fatalf("news was filed under the token's DID, which cannot read the row: %+v", left)
	}
	n := got[0]
	if n.ID != "ground:ci-status:sess-1:abc123" {
		t.Errorf("id %q; want the attestation's own and the commit, since the row is per session", n.ID)
	}
	if n.Item.Symbol != statusline.SymbolWell {
		t.Errorf("a success drew %q", n.Item.Symbol)
	}
	if !strings.Contains(n.Item.Note, "success") || !strings.Contains(n.Item.Note, "sky-whisper") {
		t.Errorf("note %q says neither the conclusion nor the branch", n.Item.Note)
	}
	if n.Detail["sha"] != "abc123" || n.Detail["conclusion"] != "success" {
		t.Errorf("detail %v", n.Detail)
	}
	if n.UntilMs <= time.Now().UnixMilli() {
		t.Errorf("news left already expired: until=%d", n.UntilMs)
	}
	// History, the commit, the runs twice, and each green run's artifacts.
	if len(gh.asked) != 6 {
		t.Errorf("github was asked %d times: %v", len(gh.asked), gh.asked)
	}
	for _, tok := range gh.tokens {
		if tok != "tok-1" {
			t.Fatalf("an ask went without the token: %v", gh.tokens)
		}
	}
	// ground's row names the commit as git's push line printed it, short.
	// The runs endpoint filters on the whole sha and a short one matches no
	// run at all, so the commit is resolved first and asked for whole.
	if !strings.Contains(gh.asked[1], "/repos/teranos/ground/commits/abc123") {
		t.Errorf("the short sha was not resolved first: %q", gh.asked[1])
	}
	if !strings.Contains(gh.asked[2], "/repos/teranos/ground/actions/runs?head_sha="+wholeSha) {
		t.Errorf("the commit's runs were asked for as %q; wanted the whole sha", gh.asked[2])
	}
}

func aRunAt(name, status, conclusion string, updated time.Time) string {
	return `{"name":"` + name + `","status":"` + status + `","conclusion":"` + conclusion + `","updated_at":"` + updated.UTC().Format(time.RFC3339) + `"}`
}

func rearmJobFor(t *testing.T, as *types.As) *async.Job {
	t.Helper()
	job := jobFor(t, as)
	job.Source = rearmSource
	return job
}

// "too much green ci success that dont really give us sifnal, it lingers too long"
// A push re-armed at boot whose runs concluded before the process began was
// said by the process before; saying it again on every restart was the green.
func TestCIWatchARearmedPushThatConcludedBeforeBootSaysNothing(t *testing.T) {
	booted := time.Now()
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{runsJSON(aRunAt("Go", "completed", "success", booted.Add(-time.Hour)),
			aRunAt("Nix", "completed", "failure", booted.Add(-time.Hour)))},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	h.booted = booted
	if err := h.Execute(context.Background(), rearmJobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, _ := news.LeftFor("https://mastodon.example/@alice"); len(got) != 0 {
		t.Fatalf("a push concluded before boot was put on the row again: %+v", got)
	}
}

// A re-armed push that concludes after boot is what the restart re-armed for.
func TestCIWatchARearmedPushThatConcludesAfterBootIsSaid(t *testing.T) {
	booted := time.Now().Add(-time.Minute)
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{
			runsJSON(aRunAt("Go", "in_progress", "", booted.Add(time.Second))),
			runsJSON(aRunAt("Go", "completed", "failure", booted.Add(30*time.Second))),
		},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	h.booted = booted
	var seen []statusline.News
	h.sleep = func(context.Context, time.Duration) error {
		seen, _ = news.LeftFor("https://mastodon.example/@alice")
		return nil
	}
	if err := h.Execute(context.Background(), rearmJobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(seen) != 1 || !seen[0].Quiet || !strings.Contains(seen[0].Item.Note, "watching sky-whisper abc123 1 runs") {
		t.Fatalf("while the re-armed run went on the row showed %+v", seen)
	}
	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 || got[0].Item.Symbol != statusline.SymbolUnwell {
		t.Fatalf("a re-armed push that failed after boot drew %+v", got)
	}
}

// A re-armed push is not on the row before its runs are read: fifteen of
// them at once said "watching" for pushes long concluded.
func TestCIWatchARearmedPushIsNotWatchedBeforeItsRunsAreRead(t *testing.T) {
	booted := time.Now()
	gh := &scriptedGitHub{history: noHistory(), commits: []string{runsJSON(aRunAt("Go", "completed", "success", booted.Add(-time.Hour)))}}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	h.booted = booted
	h.get = func(ctx context.Context, token, url string) ([]byte, error) {
		if held, _ := news.LeftFor("https://mastodon.example/@alice"); len(held) != 0 {
			t.Fatalf("on the row before its runs were read: %+v", held)
		}
		return gh.get(ctx, token, url)
	}
	if err := h.Execute(context.Background(), rearmJobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// "im more intersted in fails"
// Green leaves the row once the laptop has seen it; anything else stays the whole hold.
func TestCIWatchHoldsAFailureLongerThanASuccess(t *testing.T) {
	if holdFor("success") >= holdFor("failure") || holdFor("cancelled") != statusline.NewsHold || holdFor("failure") != statusline.NewsHold {
		t.Fatalf("success %s, failure %s, cancelled %s", holdFor("success"), holdFor("failure"), holdFor("cancelled"))
	}
	if successHold < 2*time.Second {
		t.Fatalf("a success held %s is gone before the laptop's one-second poll has looked twice", successHold)
	}
}

// A dispatch re-armed at boot that concluded before it was said before.
func TestCIWatchARearmedDispatchThatConcludedBeforeBootSaysNothing(t *testing.T) {
	booted := time.Now()
	gh := &scriptedGitHub{dispatched: []string{runsJSON(aRunAt("deploy q-deploy-1790:TARGET", "completed", "success", booted.Add(-time.Hour)))}}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	h.booted = booted
	if err := h.Execute(context.Background(), rearmJobFor(t, dispatchAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, _ := news.LeftFor("https://mastodon.example/@alice"); len(got) != 0 {
		t.Fatalf("a dispatch concluded before boot was put on the row again: %+v", got)
	}
}

// A red beside a green is red. Reading one run read whichever finished first,
// and the row said green four times over a failing lint.
func TestCIWatchARedBesideAGreenIsRed(t *testing.T) {
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{runsJSON(aRun("deploy", "completed", "success", "g"), aRun("lint", "completed", "failure", "r"))},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 || got[0].Item.Symbol != statusline.SymbolUnwell {
		t.Fatalf("a red beside a green drew %+v", got)
	}
	if !strings.Contains(got[0].Item.Note, "failure") || !strings.Contains(got[0].Item.Note, "1/2") {
		t.Errorf("note %q does not say failure, or how many", got[0].Item.Note)
	}
	if got[0].Detail["url"] != "r" {
		t.Errorf("the run to open is %v; wanted the red one", got[0].Detail["url"])
	}
}

// Nothing is said while any workflow of the push is still running.
func TestCIWatchWaitsForEveryWorkflow(t *testing.T) {
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{
			runsJSON(aRun("Go", "completed", "success", "g"), aRun("Nix", "queued", "", "n")),
			runsJSON(aRun("Go", "completed", "success", "g"), aRun("Nix", "completed", "success", "n")),
		},
	}
	news := statusline.NewNewsLog()
	slept := 0
	h := handlerOver(gh, news)
	h.sleep = func(context.Context, time.Duration) error { slept++; return nil }
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if slept != 1 {
		t.Errorf("slept %d times; wanted once, for the queued workflow", slept)
	}
	if got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli()); len(got) != 1 || got[0].Item.Symbol != statusline.SymbolWell {
		t.Fatalf("all green drew %+v", got)
	}
}

// While the run is watched the row says so, quietly: an item with no id, so
// the laptop writes nothing down and wakes nobody, gone when the verdict is
// in. From the laptop a wait and a push nobody watched looked the same.
func TestCIWatchShowsTheWaitOnTheRowWithoutAnID(t *testing.T) {
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{
			runsJSON(aRun("Go", "in_progress", "", "g")),
			runsJSON(aRun("Go", "completed", "success", "g")),
		},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	rowDuringWait := func() []statusline.StatusItem {
		a := tokenCaller("did:key:alice")
		return (&statusline.StatusLineHandler{News: func() *statusline.NewsLog { return news }}).NewsFor(a)
	}
	var seen []statusline.StatusItem
	h.sleep = func(context.Context, time.Duration) error { seen = rowDuringWait(); return nil }
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(seen) != 1 || !strings.Contains(seen[0].Note, "watching sky-whisper abc123") {
		t.Fatalf("during the wait the row showed %+v", seen)
	}
	if seen[0].ID != "" {
		t.Fatalf("the wait carried an id %q; the laptop would write it down and wake a session", seen[0].ID)
	}
	after := rowDuringWait()
	if len(after) != 1 || after[0].ID == "" || !strings.Contains(after[0].Note, "success") {
		t.Fatalf("after the verdict the row showed %+v; wanted the verdict alone, with its id", after)
	}
}

// A spent quota is waited out, not failed. The first hour on the node spent
// the person's whole 5000, and every push in flight then failed on the row.
func TestCIWatchWaitsOutASpentQuota(t *testing.T) {
	reset := time.Now().Add(90 * time.Second)
	gh := &scriptedGitHub{history: noHistory(), commits: []string{runsJSON(aRun("Go", "completed", "success", "g"))}}
	refusals := 1
	get := func(ctx context.Context, token, url string) ([]byte, error) {
		if strings.Contains(url, "head_sha=") && refusals > 0 {
			refusals--
			return nil, rateLimited{reset: reset}
		}
		return gh.get(ctx, token, url)
	}
	news := statusline.NewNewsLog()
	var slept []time.Duration
	h := handlerOver(gh, news)
	h.get = get
	h.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("a spent quota failed the push: %v", err)
	}
	if len(slept) != 1 || slept[0] < 85*time.Second || slept[0] > 95*time.Second {
		t.Fatalf("slept %v; wanted once, until the reset", slept)
	}
	if got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli()); len(got) != 1 {
		t.Fatalf("news after the quota came back: %+v", got)
	}
}

// GitHub's headers say when a spent quota comes back; a 403 that is not about
// the quota is not read as one.
func TestRateLimitResetReadsGitHubsHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", "1790201685")
	reset, ok := rateLimitReset(h)
	if !ok || reset.Unix() != 1790201685 {
		t.Fatalf("primary reset read as %v %v", reset, ok)
	}
	if _, ok := rateLimitReset(http.Header{}); ok {
		t.Fatal("a refusal with no quota headers read as the quota")
	}
	r := http.Header{}
	r.Set("Retry-After", "30")
	if reset, ok := rateLimitReset(r); !ok || time.Until(reset) < 25*time.Second {
		t.Fatalf("secondary Retry-After read as %v %v", reset, ok)
	}
}

// The wait between asks is the three comparisons ground made, ported as-is.
func TestPickAdaptiveSleepIsGroundsThreeComparisons(t *testing.T) {
	cases := []struct{ elapsed, p50, p90, want int64 }{
		{0, 0, 0, 2},
		{10, 40, 90, 30},
		{50, 40, 90, 5},
		{50, 40, 0, 5},
		{100, 40, 90, 2},
	}
	for _, c := range cases {
		if got := pickAdaptiveSleep(c.elapsed, c.p50, c.p90); got != c.want {
			t.Errorf("pickAdaptiveSleep(%d,%d,%d)=%d want %d", c.elapsed, c.p50, c.p90, got, c.want)
		}
	}
}

// The percentiles are ground's arithmetic over GitHub's timestamps: sorted
// durations, the element at half and at nine tenths.
func TestPercentilesOfIsGroundsArithmetic(t *testing.T) {
	at := func(s int) time.Time { return time.Date(2026, 9, 22, 10, 0, s, 0, time.UTC) }
	runs := []ciRun{
		{RunStartedAt: at(0), UpdatedAt: at(90)},
		{RunStartedAt: at(0), UpdatedAt: at(30)},
		{RunStartedAt: at(0), UpdatedAt: at(60)},
		{RunStartedAt: at(0), UpdatedAt: at(120)},
	}
	p50, p90 := percentilesOf(runs)
	if p50 != 90 || p90 != 120 {
		t.Errorf("p50=%d p90=%d; want 90 120 for [30 60 90 120]", p50, p90)
	}
	if p50, p90 := percentilesOf(nil); p50 != 0 || p90 != 0 {
		t.Errorf("no history is 0 0, got %d %d", p50, p90)
	}
}

// A row with no repo is not a push; the handler says so rather than asking
// github about nothing.
func TestCIWatchRefusesARowWithNoRepo(t *testing.T) {
	as := ciStatusAs("did:key:alice")
	delete(as.Attributes, "repo")
	h := handlerOver(&scriptedGitHub{history: noHistory()}, statusline.NewNewsLog())
	if err := h.Execute(context.Background(), jobFor(t, as)); err == nil {
		t.Fatal("a row naming no repo was accepted")
	}
}

// A rite dispatched a workflow and the walk moved on. The run lands on the
// default branch with no sha to find it by, so it is found by the name ground
// gave it, and the verdict goes back the way a push's does. Sky asked github
// for this from the laptop every five seconds; the laptop asks nothing now.
func dispatchAs(actor string) *types.As {
	return &types.As{
		ID:         "immediate:dispatch:sess-1:q-deploy-1790:TARGET",
		Subjects:   []string{"dispatch"},
		Predicates: []string{watcher.DispatchSentPredicate},
		Contexts:   []string{"session:sess-1"},
		Actors:     []string{actor, "ground"},
		Timestamp:  time.Now().Add(-10 * time.Second),
		Attributes: map[string]any{
			"repo":  "sbvh-nl/q.sbvh.nl",
			"token": "q-deploy-1790:TARGET",
		},
	}
}

func TestCIWatchWatchesADispatchedRunByItsName(t *testing.T) {
	ours := "https://github.com/sbvh-nl/q.sbvh.nl/actions/runs/9"
	gh := &scriptedGitHub{
		dispatched: []string{
			// Not listed yet: a run does not appear the instant it is sent.
			runsJSON(aRun("deploy other-1789:TARGET", "completed", "success", "u0")),
			runsJSON(aRun("deploy q-deploy-1790:TARGET", "in_progress", "", ours),
				aRun("deploy other-1789:TARGET", "completed", "success", "u0")),
			runsJSON(aRun("deploy q-deploy-1790:TARGET", "completed", "success", ours),
				aRun("deploy other-1789:TARGET", "completed", "success", "u0")),
		},
	}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)

	if err := h.Execute(context.Background(), jobFor(t, dispatchAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 {
		t.Fatalf("news for alice: %d items, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.ID != "immediate:dispatch:sess-1:q-deploy-1790:TARGET" {
		t.Errorf("id %q; want the row's own, one row per dispatch already", n.ID)
	}
	if !strings.Contains(n.Item.Note, "success") || !strings.Contains(n.Item.Note, "q-deploy-1790:TARGET") {
		t.Errorf("note %q says neither the conclusion nor the run's name", n.Item.Note)
	}
	if n.Detail["url"] != ours || n.Detail["conclusion"] != "success" || n.Detail["token"] != "q-deploy-1790:TARGET" {
		t.Errorf("detail %v", n.Detail)
	}
	for _, u := range gh.asked {
		if strings.Contains(u, "head_sha") || strings.Contains(u, "/commits/") || strings.Contains(u, "branch=") {
			t.Errorf("a dispatched run was looked for by a sha or branch it does not have: %q", u)
		}
		if !strings.Contains(u, "event=workflow_dispatch") {
			t.Errorf("asked %q; a dispatched run is among the dispatched", u)
		}
	}
	if len(gh.asked) != 3 {
		t.Errorf("github was asked %d times: %v", len(gh.asked), gh.asked)
	}
}

// A dispatched run that never appears is said so, not waited on for the
// push ceiling: ground gave up at sixty seconds and said "no run carries the
// name ground gave it", and that is what comes back.
func TestCIWatchSaysWhenNoRunCarriesTheName(t *testing.T) {
	gh := &scriptedGitHub{}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	as := dispatchAs("did:key:alice")
	as.Timestamp = time.Now().Add(-2 * dispatchAppear)

	if err := h.Execute(context.Background(), jobFor(t, as)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 {
		t.Fatalf("news for alice: %d items, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Item.Note, "no run carries the name") {
		t.Errorf("note %q", got[0].Item.Note)
	}
	if got[0].Item.Symbol != statusline.SymbolUnwell {
		t.Errorf("an absent run drew %q", got[0].Item.Symbol)
	}
}

// "that is a qntx responsibility"
// A push whose commit no workflow ran on: ground-knows-what-died had no PR,
// and each push of it was waited on for two hours and then called failed.
func TestCIWatchSaysWhenAPushStartedNoRun(t *testing.T) {
	gh := &scriptedGitHub{history: noHistory()}
	news := statusline.NewNewsLog()
	h := handlerOver(gh, news)
	// Waiting at all is the defect, so a wait that goes round is cut short.
	waits := 0
	h.sleep = func(ctx context.Context, d time.Duration) error {
		waits++
		if waits > 3 {
			return context.Canceled
		}
		return nil
	}
	as := ciStatusAs("did:key:alice")
	as.Timestamp = time.Now().Add(-2 * dispatchAppear)

	if err := h.Execute(context.Background(), jobFor(t, as)); err != nil {
		t.Fatalf("a push with no run was a failure: %v", err)
	}
	got := news.Since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 {
		t.Fatalf("news for alice: %d items, want 1: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Item.Note, "no CI run") || !strings.Contains(got[0].Item.Note, "sky-whisper") {
		t.Errorf("note %q", got[0].Item.Note)
	}
	if !got[0].Quiet {
		t.Error("no run is not a verdict, and no session is woken for it")
	}
}

// "can we use a github runner, hosted public one, to do ci benchmarking of the
// CTFE, and make sure those results end up in QNTX"
// ground's bench workflow uploads its row as an artifact; the node takes it.

const benchRun = "https://github.com/teranos/ground/actions/runs/41"

const benchArchive = "https://api.github.com/repos/teranos/ground/actions/artifacts/77/zip"

var benchConcluded = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

const benchJSON = `{"subjects":["ground:v0.9.1"],"predicates":["bench:ctfe"],"contexts":["github:ubuntu"],"actors":["ground-ci"],"source":"ground-ci","attributes":{"seconds":38.82,"peak_kib":472848,"ldc":"LDC - the LLVM D compiler (1.41.0):","sha":"` + wholeSha + `","version":"v0.9.1","runner":"ubuntu","run":"` + benchRun + `"}}`

func aRunWithID(id, name, conclusion, url string, updated time.Time) string {
	return `{"id":` + id + `,"name":"` + name + `","status":"completed","conclusion":"` + conclusion + `","html_url":"` + url + `","updated_at":"` + updated.UTC().Format(time.RFC3339) + `"}`
}

func artifactsJSON(name, archive string) string {
	return `{"total_count":1,"artifacts":[{"id":77,"name":"` + name + `","archive_download_url":"` + archive + `"}]}`
}

func zipOf(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A GitHub whose push ran bench, green, with its artifact holding content.
func benchGitHub(t *testing.T, artifact, file, content string, pushes int) *scriptedGitHub {
	gh := &scriptedGitHub{
		history:   noHistory(),
		artifacts: map[string]string{"41": artifactsJSON(artifact, benchArchive)},
		zips:      map[string][]byte{benchArchive: zipOf(t, file, content)},
	}
	for range pushes {
		gh.commits = append(gh.commits, runsJSON(aRunWithID("41", "bench", "success", benchRun, benchConcluded)))
	}
	servingZips(t, gh)
	return gh
}

// The store of the push row's namespace, refusing any other.
func groundStore(t *testing.T, h *ciWatchHandler) ats.AttestationStore {
	t.Helper()
	store, _ := createTestStore(t)
	h.store = func(namespace string) (ats.AttestationStore, error) {
		if namespace != "Ground" {
			t.Errorf("the bench was written to %q; the push row is in Ground", namespace)
		}
		return store, nil
	}
	return store
}

func benchesIn(t *testing.T, store ats.AttestationStore) []*types.As {
	t.Helper()
	got, err := store.GetAttestations(ats.AttestationFilter{Predicates: []string{"bench:ctfe"},
		TimeStart: types.Unbounded(), TimeEnd: types.Unbounded()})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// "ground things need to go to ground namespace on k"
// The row lands in the namespace the push row did, as the artifact gave it,
// dated when the run concluded.
func TestCIWatchTakesARunsBenchAsAnAttestation(t *testing.T) {
	gh := benchGitHub(t, "bench-ctfe", "bench-ctfe.json", benchJSON, 1)
	h := handlerOver(gh, statusline.NewNewsLog())
	store := groundStore(t, h)

	if err := h.Execute(quote.WithNamespace(context.Background(), "Ground"), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := benchesIn(t, store)
	if len(got) != 1 {
		t.Fatalf("%d bench rows, want 1: %+v", len(got), got)
	}
	as := got[0]
	if strings.Join(as.Subjects, ",") != "ground:v0.9.1" || strings.Join(as.Predicates, ",") != "bench:ctfe" ||
		strings.Join(as.Contexts, ",") != "github:ubuntu" || strings.Join(as.Actors, ",") != "ground-ci" {
		t.Errorf("the row is not the artifact's: %v %v %v %v", as.Subjects, as.Predicates, as.Contexts, as.Actors)
	}
	if as.Source != "ground-ci" {
		t.Errorf("source %q", as.Source)
	}
	if !as.Timestamp.Equal(benchConcluded) {
		t.Errorf("timestamp %s; the run concluded %s", as.Timestamp, benchConcluded)
	}
	if as.Attributes["seconds"] != 38.82 || as.Attributes["peak_kib"] != float64(472848) ||
		as.Attributes["sha"] != wholeSha || as.Attributes["run"] != benchRun ||
		as.Attributes["ldc"] != "LDC - the LLVM D compiler (1.41.0):" {
		t.Errorf("attributes %v", as.Attributes)
	}
	if len(gh.located) != 1 || gh.located[0] != benchArchive {
		t.Errorf("located %v; wanted the artifact's archive once", gh.located)
	}
}

// Most runs upload no bench; a run without one writes nothing and is no error.
func TestCIWatchARunWithoutTheBenchWritesNothing(t *testing.T) {
	gh := benchGitHub(t, "something-else", "bench-ctfe.json", benchJSON, 1)
	h := handlerOver(gh, statusline.NewNewsLog())
	store := groundStore(t, h)

	if err := h.Execute(quote.WithNamespace(context.Background(), "Ground"), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("a run with no bench failed the push: %v", err)
	}
	if got := benchesIn(t, store); len(got) != 0 {
		t.Fatalf("a run with no bench wrote %+v", got)
	}
	if len(gh.located) != 0 {
		t.Errorf("an artifact that is not the bench was downloaded: %v", gh.located)
	}
}

// A push re-armed at boot reads the same artifact again, and writes it once.
func TestCIWatchARearmedPushWritesTheBenchOnce(t *testing.T) {
	gh := benchGitHub(t, "bench-ctfe", "bench-ctfe.json", benchJSON, 2)
	h := handlerOver(gh, statusline.NewNewsLog())
	h.booted = benchConcluded.Add(-time.Minute)
	store := groundStore(t, h)
	ctx := quote.WithNamespace(context.Background(), "Ground")

	if err := h.Execute(ctx, jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := h.Execute(ctx, rearmJobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute, re-armed: %v", err)
	}
	if got := benchesIn(t, store); len(got) != 1 {
		t.Fatalf("%d bench rows after a re-arm, want 1: %+v", len(got), got)
	}
}

// An artifact named the bench that does not hold a row is said, with the run.
func TestCIWatchABenchThatIsNoRowIsAnError(t *testing.T) {
	for name, held := range map[string][2]string{
		"no file":    {"other.json", benchJSON},
		"not a row":  {"bench-ctfe.json", `["ground:v0.9.1"]`},
		"no subject": {"bench-ctfe.json", `{"predicates":["bench:ctfe"],"contexts":["github:ubuntu"],"actors":["ground-ci"]}`},
	} {
		t.Run(name, func(t *testing.T) {
			gh := benchGitHub(t, "bench-ctfe", held[0], held[1], 1)
			h := handlerOver(gh, statusline.NewNewsLog())
			store := groundStore(t, h)
			err := h.Execute(quote.WithNamespace(context.Background(), "Ground"), jobFor(t, ciStatusAs("did:key:alice")))
			if err == nil || !strings.Contains(err.Error(), benchRun) {
				t.Fatalf("got %v; wanted an error naming %s", err, benchRun)
			}
			if got := benchesIn(t, store); len(got) != 0 {
				t.Fatalf("wrote %+v", got)
			}
		})
	}
}
