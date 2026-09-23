package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// A GitHub that answers from a script: the branch's history once, then the
// commit's runs for each ask until they conclude. Every ask is remembered.
type scriptedGitHub struct {
	history string
	commits []string
	asked   []string
	tokens  []string
}

const wholeSha = "abc123def4567890abc123def4567890abc123de"

func (g *scriptedGitHub) get(_ context.Context, token, url string) ([]byte, error) {
	g.asked = append(g.asked, url)
	g.tokens = append(g.tokens, token)
	if strings.Contains(url, "branch=") {
		return []byte(g.history), nil
	}
	// The commits endpoint answers a prefix with the whole sha.
	if strings.Contains(url, "/commits/abc123") {
		return []byte(`{"sha":"` + wholeSha + `"}`), nil
	}
	if len(g.commits) == 0 {
		return []byte(`{"workflow_runs":[]}`), nil
	}
	next := g.commits[0]
	g.commits = g.commits[1:]
	return []byte(next), nil
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
		Attributes: map[string]interface{}{
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

func handlerOver(gh *scriptedGitHub, news *newsLog) *ciWatchHandler {
	return &ciWatchHandler{
		get:   gh.get,
		token: func(context.Context) (string, error) { return "tok-1", nil },
		sleep: func(context.Context, time.Duration) error { return nil },
		news:  news,
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
	news := newNewsLog()
	h := handlerOver(gh, news)

	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := news.since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 {
		t.Fatalf("news for alice: %d items, want 1: %+v", len(got), got)
	}
	if left := news.since("did:key:alice", time.Now().UnixMilli()); len(left) != 0 {
		t.Fatalf("news was filed under the token's DID, which cannot read the row: %+v", left)
	}
	n := got[0]
	if n.ID != "ground:ci-status:sess-1:abc123" {
		t.Errorf("id %q; want the attestation's own and the commit, since the row is per session", n.ID)
	}
	if n.Item.Symbol != SymbolWell {
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
	if len(gh.asked) != 4 {
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

// A red beside a green is red. Reading one run read whichever finished first,
// and the row said green four times over a failing lint.
func TestCIWatchARedBesideAGreenIsRed(t *testing.T) {
	gh := &scriptedGitHub{
		history: noHistory(),
		commits: []string{runsJSON(aRun("deploy", "completed", "success", "g"), aRun("lint", "completed", "failure", "r"))},
	}
	news := newNewsLog()
	h := handlerOver(gh, news)
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := news.since("https://mastodon.example/@alice", time.Now().UnixMilli())
	if len(got) != 1 || got[0].Item.Symbol != SymbolUnwell {
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
	news := newNewsLog()
	slept := 0
	h := handlerOver(gh, news)
	h.sleep = func(context.Context, time.Duration) error { slept++; return nil }
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if slept != 1 {
		t.Errorf("slept %d times; wanted once, for the queued workflow", slept)
	}
	if got := news.since("https://mastodon.example/@alice", time.Now().UnixMilli()); len(got) != 1 || got[0].Item.Symbol != SymbolWell {
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
	news := newNewsLog()
	h := handlerOver(gh, news)
	rowDuringWait := func() []StatusItem {
		a := tokenCaller("did:key:alice")
		return (&StatusLineHandler{news: func() *newsLog { return news }}).newsFor(a)
	}
	var seen []StatusItem
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
	news := newNewsLog()
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
	if got := news.since("https://mastodon.example/@alice", time.Now().UnixMilli()); len(got) != 1 {
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
	h := handlerOver(&scriptedGitHub{history: noHistory()}, newNewsLog())
	if err := h.Execute(context.Background(), jobFor(t, as)); err == nil {
		t.Fatal("a row naming no repo was accepted")
	}
}
