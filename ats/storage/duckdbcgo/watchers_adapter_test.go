//go:build cgo && rustduckdb

package duckdbcgo

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

func newWatchers(t *testing.T, location string) *Watchers {
	t.Helper()
	return NewWatchers(newWatcherStore(t, location))
}

func sampleWatcher(id string) *storage.Watcher {
	return &storage.Watcher{
		ID:                id,
		Name:              "watcher " + id,
		Filter:            types.AxFilter{Predicates: []string{"thing:happened"}, Contexts: []string{"here"}},
		ActionType:        storage.ActionTypeWebhook,
		ActionData:        "http://127.0.0.1:1/hook",
		MaxFiresPerSecond: 8,
		Enabled:           true,
		AttributeFilters: []storage.AttributeFilter{
			{Path: "tool_name", Op: "equals", Value: "bash"},
		},
	}
}

// A filter is what a watcher is for. Losing it across the boundary would give
// back a watcher that matches everything.
func TestAdapterKeepsTheFilter(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())

	if err := w.Create(ctx, sampleWatcher("w1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := w.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("the watcher did not come back")
	}
	if len(got.Filter.Predicates) != 1 || got.Filter.Predicates[0] != "thing:happened" {
		t.Fatalf("predicates lost: %+v", got.Filter)
	}
	if len(got.Filter.Contexts) != 1 || got.Filter.Contexts[0] != "here" {
		t.Fatalf("contexts lost: %+v", got.Filter)
	}
	if len(got.AttributeFilters) != 1 || got.AttributeFilters[0].Path != "tool_name" {
		t.Fatalf("attribute filters lost: %+v", got.AttributeFilters)
	}
	if got.ActionType != storage.ActionTypeWebhook || got.MaxFiresPerSecond != 8 {
		t.Fatalf("action lost: %+v", got)
	}
}

// The counters come from the fire stream, not from the declaration.
func TestAdapterCountersComeFromTheStream(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())
	if err := w.Create(ctx, sampleWatcher("w1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := w.RecordFire(ctx, "w1", "AS-DUCK-SWAM-POND-7K4M"); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if err := w.RecordFire(ctx, "w1", "AS-DUCK-SWAM-POND-7K4M"); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if err := w.RecordError(ctx, "w1", "webhook returned 500", "AS-DUCK-SANK-POND-9X2B"); err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	got, err := w.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FireCount != 2 || got.ErrorCount != 1 {
		t.Fatalf("wrong counters: fires=%d errors=%d", got.FireCount, got.ErrorCount)
	}
	if got.LastError != "webhook returned 500" {
		t.Fatalf("wrong last error: %q", got.LastError)
	}
	if got.LastFiredAt == nil {
		t.Fatal("no last fired time")
	}
}

// Updating a declaration must not reset what the watcher has done.
func TestAdapterUpdateKeepsTheCounters(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())
	if err := w.Create(ctx, sampleWatcher("w1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.RecordFire(ctx, "w1", "AS-DUCK-SWAM-POND-7K4M"); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}

	changed := sampleWatcher("w1")
	changed.Filter.Predicates = []string{"other:thing"}
	if err := w.Update(ctx, changed); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := w.Get(ctx, "w1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FireCount != 1 {
		t.Fatalf("the update reset the tally: %d", got.FireCount)
	}
	if got.Filter.Predicates[0] != "other:thing" {
		t.Fatalf("the update did not take: %+v", got.Filter)
	}
}

func TestAdapterCreateRefusesADuplicate(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())
	if err := w.Create(ctx, sampleWatcher("w1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Create(ctx, sampleWatcher("w1")); err == nil {
		t.Fatal("declaring the same watcher twice reported success")
	}
}

func TestAdapterListHonoursEnabledOnly(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())

	off := sampleWatcher("w2")
	off.Enabled = false
	if err := w.Create(ctx, sampleWatcher("w1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Create(ctx, off); err != nil {
		t.Fatalf("Create: %v", err)
	}

	all, err := w.List(ctx, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	enabled, err := w.List(ctx, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || len(enabled) != 1 || enabled[0].ID != "w1" {
		t.Fatalf("wrong lists: all=%d enabled=%d", len(all), len(enabled))
	}
}

func TestAdapterDeleteByPrefix(t *testing.T) {
	ctx := context.Background()
	w := newWatchers(t, t.TempDir())
	for _, id := range []string{"se-glyph-a", "se-glyph-b", "keep-me"} {
		if err := w.Create(ctx, sampleWatcher(id)); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	removed, err := w.DeleteByPrefix(ctx, "se-glyph-")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected two withdrawn, got %d", removed)
	}

	left, err := w.List(ctx, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(left) != 1 || left[0].ID != "keep-me" {
		t.Fatalf("wrong survivors: %+v", left)
	}
}

// An id nothing is declared under is nil without an error, which is what the
// SQLite store does and what every caller checks for.
func TestAdapterGetUnknownIsNil(t *testing.T) {
	got, err := newWatchers(t, t.TempDir()).Get(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}
