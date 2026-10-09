package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// removalJob is what ground attests for a commit: the spans it took out and
// put back nowhere, each beside the file it left.
func removalJob(t *testing.T, files, spans []string) *async.Job {
	t.Helper()
	f := make([]any, 0, len(files))
	for _, s := range files {
		f = append(f, s)
	}
	s := make([]any, 0, len(spans))
	for _, x := range spans {
		s = append(s, x)
	}
	as := types.As{
		ID:         "removal-1",
		Predicates: []string{watcher.QuoteRemovedPredicate},
		Contexts:   []string{"session:sess-1"},
		Actors:     []string{"did:key:ground"},
		Attributes: map[string]any{"commit": "abc1234", "files": f, "spans": s},
	}
	payload, err := json.Marshal(as)
	if err != nil {
		t.Fatal(err)
	}
	return &async.Job{ID: "builtin:quote-removal", HandlerName: watcher.QuoteRemovalHandlerName, Payload: payload}
}

func removalHandlerWith(prompts ...string) (*quoteRemovalHandler, *newsLog, *string) {
	asked := new(string)
	news := newNewsLog()
	return &quoteRemovalHandler{
		prompts: func(ns string) ([]string, error) {
			*asked = ns
			return prompts, nil
		},
		news:     news,
		mintedBy: func(did string) (string, bool) { return "person-1", true },
		logger:   zap.NewNop().Sugar(),
	}, news, asked
}

func TestQuoteRemovalReportsWhatTheUserSaid(t *testing.T) {
	h, news, asked := removalHandlerWith("the max time on phases is ideally lower than 5s always")
	ctx := withNamespace(context.Background(), "ground")
	job := removalJob(t,
		[]string{"source/a.d", "source/b.d"},
		[]string{"the max time on phases is ideally lower than 5s always", "words the agent wrote itself"})
	if err := h.Execute(ctx, job); err != nil {
		t.Fatal(err)
	}
	if *asked != "ground" {
		t.Errorf("prompts were read in %q, not the namespace the commit landed in", *asked)
	}
	held := news.since("person-1", time.Now().UnixMilli())
	if len(held) != 1 {
		t.Fatalf("want one item for the person, got %d", len(held))
	}
	note := held[0].Item.Note
	if !strings.Contains(note, "the max time on phases is ideally lower than 5s always") {
		t.Errorf("the note does not name the quote: %q", note)
	}
	if !strings.Contains(note, "abc1234") || !strings.Contains(note, "source/a.d") {
		t.Errorf("the note does not say which commit took it out of which file: %q", note)
	}
	if held[0].Detail["session"] != "sess-1" {
		t.Errorf("the item is not for the session that committed: %v", held[0].Detail["session"])
	}
	removed, _ := held[0].Detail["removed"].([]quoteRemoved)
	if len(removed) != 1 || removed[0].File != "source/a.d" || removed[0].Verdict != "sourced" {
		t.Errorf("removed = %+v", removed)
	}
}

// A quote the user never said was not theirs to lose.
func TestQuoteRemovalOfUnsourcedLeavesNothing(t *testing.T) {
	h, news, _ := removalHandlerWith("something else entirely")
	ctx := withNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, removalJob(t, []string{"a.d"}, []string{"words the agent wrote itself"})); err != nil {
		t.Fatal(err)
	}
	if held := news.since("person-1", time.Now().UnixMilli()); len(held) != 0 {
		t.Errorf("an unsourced removal left %d items", len(held))
	}
}

// Within the warn band the claim side passes the span as the user's, so the
// removal side reports it as theirs.
func TestQuoteRemovalReportsAStretchedQuote(t *testing.T) {
	said := "you replace something i said with something else, this is a different kind of violation"
	span := "you replac somethin i sai wth somethng els, ths is a diferent knd of violaton"
	if v := verdictOf(span, []string{said}); v != quoteStretched {
		t.Fatalf("the fixture is %s, not stretched", v)
	}
	h, news, _ := removalHandlerWith(said)
	ctx := withNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, removalJob(t, []string{"a.md"}, []string{span})); err != nil {
		t.Fatal(err)
	}
	held := news.since("person-1", time.Now().UnixMilli())
	if len(held) != 1 {
		t.Fatalf("want one item, got %d", len(held))
	}
}

func TestQuoteRemovalRefusesWhatItCannotRead(t *testing.T) {
	h, _, _ := removalHandlerWith("anything")
	if err := h.Execute(context.Background(), removalJob(t, []string{"a.d"}, []string{"anything at all"})); err == nil {
		t.Error("a removal with no namespace was answered as if its prompts had been read")
	}
	ctx := withNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, removalJob(t, []string{"a.d"}, []string{"one", "two"})); err == nil {
		t.Error("spans with no file each to name were answered")
	}
}

func TestStandingQuoteRemovedRowWatchesRemovals(t *testing.T) {
	var row *storage.Watcher
	for _, w := range watcher.Standing() {
		if w.ID == watcher.StandingQuoteRemoved {
			row = w
		}
	}
	if row == nil {
		t.Fatal("no standing row watches quote removals")
	}
	removal := &types.As{ID: "r", Predicates: []string{watcher.QuoteRemovedPredicate}}
	if !watcher.StandingMatches(removal, row) {
		t.Error("the standing row does not match a removal")
	}
	if !strings.Contains(row.ActionData, watcher.QuoteRemovalHandlerName) {
		t.Errorf("the standing row reaches %s, not the removal built-in", row.ActionData)
	}
}
