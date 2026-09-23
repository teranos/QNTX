package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// A gh that answers from a script: the percentiles once, then the run's state
// for each ask until it concludes.
type scriptedGh struct {
	percentiles string
	states      []string
	asked       []string
}

func (g *scriptedGh) run(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	g.asked = append(g.asked, joined)
	if strings.Contains(joined, "startedAt,updatedAt") {
		return []byte(g.percentiles), nil
	}
	if len(g.states) == 0 {
		return []byte(`[]`), nil
	}
	next := g.states[0]
	g.states = g.states[1:]
	return []byte(next), nil
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

// The run concludes; one item is left on the row, for the token that attested
// the push, under the attestation's own id.
func TestCIWatchLeavesNewsWhenTheRunConcludes(t *testing.T) {
	gh := &scriptedGh{
		percentiles: "40 90\n",
		states: []string{
			`[{"status":"completed","conclusion":"success","name":"Go","url":"https://github.com/teranos/ground/actions/runs/1"},` +
				`{"status":"in_progress","conclusion":"","name":"Nix","url":"https://github.com/teranos/ground/actions/runs/2"}]`,
			`[{"status":"completed","conclusion":"success","name":"Go","url":"https://github.com/teranos/ground/actions/runs/1"},` +
				`{"status":"completed","conclusion":"success","name":"Nix","url":"https://github.com/teranos/ground/actions/runs/2"}]`,
		},
	}
	news := newNewsLog()
	h := &ciWatchHandler{
		run:   gh.run,
		news:  news,
		sleep: func(context.Context, time.Duration) error { return nil },
		// The push was attested by alice's ground token; the news is hers.
		mintedBy: func(did string) (string, bool) {
			if did == "did:key:alice" {
				return "https://mastodon.example/@alice", true
			}
			return "", false
		},
		logger: zap.NewNop().Sugar(),
	}

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
	if len(gh.asked) != 3 {
		t.Errorf("gh was asked %d times: %v", len(gh.asked), gh.asked)
	}
}

// A red beside a green is red. Reading one run read whichever finished first,
// and the row said green four times over a failing lint.
func TestCIWatchARedBesideAGreenIsRed(t *testing.T) {
	gh := &scriptedGh{
		percentiles: "0 0\n",
		states: []string{
			`[{"status":"completed","conclusion":"success","name":"deploy","url":"g"},` +
				`{"status":"completed","conclusion":"failure","name":"lint","url":"r"}]`,
		},
	}
	news := newNewsLog()
	h := &ciWatchHandler{run: gh.run, news: news, sleep: func(context.Context, time.Duration) error { return nil }, logger: zap.NewNop().Sugar()}
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := news.since("did:key:alice", time.Now().UnixMilli())
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
	gh := &scriptedGh{
		percentiles: "0 0\n",
		states: []string{
			`[{"status":"completed","conclusion":"success","name":"Go","url":"g"},{"status":"queued","conclusion":"","name":"Nix","url":"n"}]`,
			`[{"status":"completed","conclusion":"success","name":"Go","url":"g"},{"status":"completed","conclusion":"success","name":"Nix","url":"n"}]`,
		},
	}
	news := newNewsLog()
	slept := 0
	h := &ciWatchHandler{run: gh.run, news: news, sleep: func(context.Context, time.Duration) error { slept++; return nil }, logger: zap.NewNop().Sugar()}
	if err := h.Execute(context.Background(), jobFor(t, ciStatusAs("did:key:alice"))); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if slept != 1 {
		t.Errorf("slept %d times; wanted once, for the queued workflow", slept)
	}
	if got := news.since("did:key:alice", time.Now().UnixMilli()); len(got) != 1 || got[0].Item.Symbol != SymbolWell {
		t.Fatalf("all green drew %+v", got)
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

// A row with no repo is not a push; the handler says so rather than asking
// github about nothing.
func TestCIWatchRefusesARowWithNoRepo(t *testing.T) {
	as := ciStatusAs("did:key:alice")
	delete(as.Attributes, "repo")
	h := &ciWatchHandler{run: (&scriptedGh{}).run, news: newNewsLog(), sleep: func(context.Context, time.Duration) error { return nil }, logger: zap.NewNop().Sugar()}
	if err := h.Execute(context.Background(), jobFor(t, as)); err == nil {
		t.Fatal("a row naming no repo was accepted")
	}
}
