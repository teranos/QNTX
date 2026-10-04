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

func claimJob(t *testing.T, spans ...string) *async.Job {
	t.Helper()
	list := make([]any, 0, len(spans))
	for _, s := range spans {
		list = append(list, s)
	}
	as := types.As{
		ID:         "claim-1",
		Predicates: []string{watcher.QuoteClaimedPredicate},
		Contexts:   []string{"session:sess-1"},
		Actors:     []string{"did:key:ground"},
		Attributes: map[string]any{"file_path": "/x/notes.md", "spans": list},
	}
	payload, err := json.Marshal(as)
	if err != nil {
		t.Fatal(err)
	}
	return &async.Job{ID: "builtin:quote", HandlerName: watcher.QuoteProvenanceHandlerName, Payload: payload}
}

func quoteHandlerWith(prompts ...string) (*quoteHandler, *newsLog, *string) {
	asked := new(string)
	news := newNewsLog()
	return &quoteHandler{
		prompts: func(ns string) ([]string, error) {
			*asked = ns
			return prompts, nil
		},
		news:     news,
		mintedBy: func(did string) (string, bool) { return "person-1", true },
		logger:   zap.NewNop().Sugar(),
	}, news, asked
}

func TestQuoteProvenanceSaysWhatHasNoSource(t *testing.T) {
	h, news, asked := quoteHandlerWith("i said this exactly once")
	ctx := withNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, claimJob(t, "said this exactly", "nobody ever typed these words")); err != nil {
		t.Fatal(err)
	}
	if *asked != "ground" {
		t.Errorf("prompts were read in %q, not the namespace the claim landed in", *asked)
	}
	held := news.since("person-1", time.Now().UnixMilli())
	if len(held) != 1 {
		t.Fatalf("want one item for the person, got %d", len(held))
	}
	if !strings.Contains(held[0].Item.Note, "nobody ever typed these words") {
		t.Errorf("the note does not name the span: %q", held[0].Item.Note)
	}
	if held[0].Detail["session"] != "sess-1" {
		t.Errorf("the item is not for the session that wrote it: %v", held[0].Detail["session"])
	}
	unsourced, _ := held[0].Detail["unsourced"].([]string)
	if len(unsourced) != 1 || unsourced[0] != "nobody ever typed these words" {
		t.Errorf("unsourced = %v", unsourced)
	}
}

func TestQuoteProvenanceSourcedLeavesNothing(t *testing.T) {
	h, news, _ := quoteHandlerWith("i said this exactly once")
	ctx := withNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, claimJob(t, "said this exactly")); err != nil {
		t.Fatal(err)
	}
	if held := news.since("person-1", time.Now().UnixMilli()); len(held) != 0 {
		t.Errorf("a sourced quote left %d items", len(held))
	}
}

func TestQuoteProvenanceRefusesWithoutANamespace(t *testing.T) {
	h, _, _ := quoteHandlerWith("anything")
	if err := h.Execute(context.Background(), claimJob(t, "anything at all here")); err == nil {
		t.Error("a claim with no namespace was answered as if its prompts had been read")
	}
}

func TestStandingQuoteRowWatchesClaims(t *testing.T) {
	var row *storage.Watcher
	for _, w := range watcher.Standing() {
		if w.ID == watcher.StandingQuoteClaimed {
			row = w
		}
	}
	if row == nil {
		t.Fatal("no standing row watches quote claims")
	}
	claim := &types.As{ID: "c", Predicates: []string{watcher.QuoteClaimedPredicate}}
	if !watcher.StandingMatches(claim, row) {
		t.Error("the standing row does not match a claim")
	}
	if !strings.Contains(row.ActionData, watcher.QuoteProvenanceHandlerName) {
		t.Errorf("the standing row reaches %s, not the quote built-in", row.ActionData)
	}
}

// fakeBuiltins records the namespace each built-in was run in.
type fakeBuiltins struct{ namespaces []string }

func (f *fakeBuiltins) ExecuteBuiltin(ctx context.Context, name string, as *types.As) error {
	f.namespaces = append(f.namespaces, namespaceOf(ctx))
	return nil
}

func TestStandingObserverNamesItsNamespace(t *testing.T) {
	f := &fakeBuiltins{}
	o := &standingObserver{
		ctx:       context.Background(),
		builtins:  func() watcher.BuiltinExecutor { return f },
		namespace: "ground",
		logger:    zap.NewNop().Sugar(),
	}
	o.OnAttestationCreated(&types.As{ID: "c", Predicates: []string{watcher.QuoteClaimedPredicate}})
	if len(f.namespaces) != 1 || f.namespaces[0] != "ground" {
		t.Errorf("the built-in ran in %v, not in the namespace the row landed in", f.namespaces)
	}
}
