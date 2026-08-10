//go:build cgo && rustduckdb

package duckdbcgo

import (
	"testing"
)

// The crate has its own tests; these prove the boundary between them —
// a declaration written from Go comes back from Go, and a fire counted in
// Rust is readable through the same handle.

func newWatcherStore(t *testing.T, location string) *WatcherStore {
	t.Helper()
	store, err := NewWatcherStore("file://"+location, NamespaceDefault)
	if err != nil {
		t.Fatalf("NewWatcherStore: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func declaration(id string) WatcherRecord {
	return WatcherRecord{
		ID:                id,
		Name:              "watcher " + id,
		ActionType:        "webhook",
		ActionData:        "http://127.0.0.1:1/hook",
		AxQuery:           "thing:happened",
		MaxFiresPerSecond: 8,
		Enabled:           true,
		CreatedAt:            1_700_000_000_000,
		UpdatedAt:            1_700_000_000_000,
		FilterJSON:           `{"predicates":["thing:happened"]}`,
		AttributeFiltersJSON: "[]",
	}
}

func TestWatcherDeclarationCrossesTheBoundary(t *testing.T) {
	dir := t.TempDir()
	store := newWatcherStore(t, dir)

	if err := store.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one declaration, got %d", len(listed))
	}
	if listed[0] != declaration("w1") {
		t.Fatalf("the record came back changed: %+v", listed[0])
	}
}

func TestWatcherDeclarationSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	first := newWatcherStore(t, dir)
	if err := first.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	first.Close()

	listed, err := newWatcherStore(t, dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "w1" {
		t.Fatalf("the declaration did not survive: %+v", listed)
	}
}

func TestWatcherTallyCountsFires(t *testing.T) {
	dir := t.TempDir()
	store := newWatcherStore(t, dir)
	if err := store.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := store.RecordFire("w1", 1_700_000_001_000); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if err := store.RecordError("w1", 1_700_000_002_000, "webhook returned 500"); err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	tally, err := store.Tally("w1")
	if err != nil {
		t.Fatalf("Tally: %v", err)
	}
	if tally.FireCount != 1 || tally.ErrorCount != 1 {
		t.Fatalf("wrong tally: %+v", tally)
	}
	if tally.LastFiredAt == nil || *tally.LastFiredAt != 1_700_000_001_000 {
		t.Fatalf("wrong last fired: %+v", tally.LastFiredAt)
	}
	if tally.LastError == nil || *tally.LastError != "webhook returned 500" {
		t.Fatalf("wrong last error: %+v", tally.LastError)
	}
}

func TestWatcherTallySurvivesReopenAfterFlush(t *testing.T) {
	dir := t.TempDir()
	first := newWatcherStore(t, dir)
	if err := first.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.RecordFire("w1", 1_700_000_001_000); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	if err := first.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	first.Close()

	tally, err := newWatcherStore(t, dir).Tally("w1")
	if err != nil {
		t.Fatalf("Tally: %v", err)
	}
	if tally.FireCount != 1 {
		t.Fatalf("the fire did not survive: %+v", tally)
	}
}

// Close flushes, so a node that stops between ticks does not lose the fires
// the last tick buffered.
func TestWatcherCloseFlushesBufferedFires(t *testing.T) {
	dir := t.TempDir()
	first := newWatcherStore(t, dir)
	if err := first.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.RecordFire("w1", 1_700_000_001_000); err != nil {
		t.Fatalf("RecordFire: %v", err)
	}
	first.Close()

	tally, err := newWatcherStore(t, dir).Tally("w1")
	if err != nil {
		t.Fatalf("Tally: %v", err)
	}
	if tally.FireCount != 1 {
		t.Fatalf("Close dropped a buffered fire: %+v", tally)
	}
}

func TestWithdrawnWatcherIsGoneAfterReopen(t *testing.T) {
	dir := t.TempDir()
	first := newWatcherStore(t, dir)
	if err := first.Put(declaration("w1")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := first.Delete("w1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	first.Close()

	listed, err := newWatcherStore(t, dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("the tombstone did not take: %+v", listed)
	}
}

// A delete that hit nothing must not read as done.
func TestWithdrawingAnUnknownWatcherIsAnError(t *testing.T) {
	store := newWatcherStore(t, t.TempDir())
	if err := store.Delete("nobody"); err == nil {
		t.Fatal("withdrawing an unknown watcher reported success")
	}
}
