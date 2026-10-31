package quote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/statusline"
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

func watchWith(prompts ...string) (*Watch, *statusline.NewsLog, *string) {
	asked := new(string)
	news := statusline.NewNewsLog()
	return &Watch{
		prompts: func(ns string) ([]Said, error) {
			*asked = ns
			return saidOf(prompts...), nil
		},
		measure:  substringMeasure,
		news:     news,
		mintedBy: func(did string) (string, bool) { return "person-1", true },
		logger:   zap.NewNop().Sugar(),
	}, news, asked
}

func TestQuoteProvenanceSaysWhatHasNoSource(t *testing.T) {
	h, news, asked := watchWith("i said this exactly once")
	ctx := WithNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, claimJob(t, "said this exactly", "nobody ever typed these words")); err != nil {
		t.Fatal(err)
	}
	if *asked != "ground" {
		t.Errorf("prompts were read in %q, not the namespace the claim landed in", *asked)
	}
	held := news.Since("person-1", time.Now().UnixMilli())
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
	// To what degree: the corrections to the nearest prompt, over the span's length.
	if !strings.Contains(held[0].Item.Note, "corrections over 29 characters") {
		t.Errorf("the note does not say to what degree: %q", held[0].Item.Note)
	}
	degrees, _ := held[0].Detail["degrees"].([]degree)
	if len(degrees) != 1 || degrees[0].Span != "nobody ever typed these words" || degrees[0].Length != 29 || degrees[0].Corrections <= 0 {
		t.Errorf("degrees = %+v", degrees)
	}
}

func TestQuoteProvenanceSourcedLeavesNothing(t *testing.T) {
	h, news, _ := watchWith("i said this exactly once")
	ctx := WithNamespace(context.Background(), "ground")
	if err := h.Execute(ctx, claimJob(t, "said this exactly")); err != nil {
		t.Fatal(err)
	}
	if held := news.Since("person-1", time.Now().UnixMilli()); len(held) != 0 {
		t.Errorf("a sourced quote left %d items", len(held))
	}
}

func TestQuoteProvenanceRefusesWithoutANamespace(t *testing.T) {
	h, _, _ := watchWith("anything")
	if err := h.Execute(context.Background(), claimJob(t, "anything at all here")); err == nil {
		t.Error("a claim with no namespace was answered as if its prompts had been read")
	}
}
