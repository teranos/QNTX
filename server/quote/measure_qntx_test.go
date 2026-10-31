//go:build qntxquote

package quote

import (
	"context"
	"strings"
	"testing"
	"time"

	quoted "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
)

// A stretched span is reported by the removal side as the user's: measured by
// Rust, not a stand-in.
func TestQuoteRemovalReportsAStretchedQuoteMeasuredByRust(t *testing.T) {
	said := "you replace something i said with something else, this is a different kind of violation"
	span := "you replac somethin i sai wth somethng els, ths is a diferent knd of violaton"
	if got, err := quoted.Measure([]string{span}, []string{said}); err != nil || kind(got[0]) != "stretched" {
		t.Fatalf("the fixture is %v (%v), not stretched", got, err)
	}
	h, news, _ := removalWith(said)
	h.measure = quoted.Measure
	if err := h.Execute(WithNamespace(context.Background(), "ground"), removalJob(t, []string{"a.md"}, []string{span})); err != nil {
		t.Fatal(err)
	}
	if held := news.Since("person-1", time.Now().UnixMilli()); len(held) != 1 {
		t.Fatalf("want one item, got %d", len(held))
	}
}

// The standing rows are matched in Rust, so these need it.
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
	if matched, err := watcher.StandingMatches(claim, row); err != nil || !matched {
		t.Errorf("the standing row does not match a claim: %v", err)
	}
	if !strings.Contains(row.ActionData, watcher.QuoteProvenanceHandlerName) {
		t.Errorf("the standing row reaches %s, not the quote built-in", row.ActionData)
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
	if matched, err := watcher.StandingMatches(removal, row); err != nil || !matched {
		t.Errorf("the standing row does not match a removal: %v", err)
	}
	if !strings.Contains(row.ActionData, watcher.QuoteRemovalHandlerName) {
		t.Errorf("the standing row reaches %s, not the removal built-in", row.ActionData)
	}
}
