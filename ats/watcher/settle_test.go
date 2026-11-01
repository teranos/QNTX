package watcher_test

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// Matched by filter rather than by an AX query: a query is parsed in Rust,
// which a build without qntxwasm does not carry.
func unbounded() types.AxFilter {
	return types.AxFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{"settle"}}
}

func queuedFor(t *testing.T, q *watcher.QueueStore, id string) int {
	t.Helper()
	stats, err := q.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	return stats.PerWatcher[id]
}

// An AX element's watcher was a Python watcher with no code, and every match
// it queued waited on a python plugin forever. Started, the engine finds it a
// tell, and what it queued is gone.
func TestStartMakesAnAXWatcherThatRunsNoCodeATell(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	ax := &storage.Watcher{ID: "ax-element-ax-1", Name: "AX Element: is",
		ActionType: storage.ActionTypePython, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	py := &storage.Watcher{ID: "ax-element-ax-2", Name: "AX with code",
		ActionType: storage.ActionTypePython, ActionData: "print(attestation)", MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	for _, w := range []*storage.Watcher{ax, py} {
		if err := store.Create(ctx, w); err != nil {
			t.Fatalf("Create %s failed: %v", w.ID, err)
		}
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	q := engine.GetQueueStore()
	for _, id := range []string{ax.ID, py.ID} {
		if err := q.Enqueue(&watcher.QueueEntry{WatcherID: id, AttestationJSON: `{"id":"AS-1"}`,
			Reason: "paused", Attempt: 1, NotBefore: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("Enqueue for %s failed: %v", id, err)
		}
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	got, err := store.Get(ctx, ax.ID)
	if err != nil {
		t.Fatalf("Get %s failed: %v", ax.ID, err)
	}
	if got.ActionType != storage.ActionTypeTell {
		t.Errorf("AX watcher with no code is %q, want tell", got.ActionType)
	}
	if n := queuedFor(t, q, ax.ID); n != 0 {
		t.Errorf("AX watcher still has %d queued", n)
	}

	// A Python watcher that has code to run is somebody's, and is left be.
	got, err = store.Get(ctx, py.ID)
	if err != nil {
		t.Fatalf("Get %s failed: %v", py.ID, err)
	}
	if got.ActionType != storage.ActionTypePython {
		t.Errorf("watcher with code is %q, want python", got.ActionType)
	}
	if n := queuedFor(t, q, py.ID); n != 1 {
		t.Errorf("watcher with code has %d queued, want 1", n)
	}
}

// A row queued for a tell before it was one is done, not an unknown action
// retried five times.
func TestDrainCompletesWhatATellQueued(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	w := &storage.Watcher{ID: "ax-element-ax-3", Name: "AX",
		ActionType: storage.ActionTypeTell, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	if err := store.Create(ctx, w); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	q := engine.GetQueueStore()
	if err := q.Enqueue(&watcher.QueueEntry{WatcherID: w.ID, AttestationJSON: `{"id":"AS-2"}`,
		Reason: "rate_limited", NotBefore: time.Now().Add(-time.Second)}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Done, and nothing put back: an unknown action is completed too, and
	// queued again a second later as a retry.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var completed, pending int
		if err := db.QueryRow(`SELECT
			COUNT(*) FILTER (WHERE status = 'completed'),
			COUNT(*) FILTER (WHERE status IN ('queued', 'running'))
			FROM watcher_execution_queue WHERE watcher_id = ?`, w.ID).Scan(&completed, &pending); err != nil {
			t.Fatalf("reading the queue failed: %v", err)
		}
		if completed > 0 && pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the drain never finished what the tell queued: %d completed, %d pending", completed, pending)
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(3 * time.Second)
	if n := queuedFor(t, q, w.ID); n != 0 {
		t.Errorf("the tell has %d queued again", n)
	}
	got, err := store.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ErrorCount != 0 {
		t.Errorf("draining a tell recorded an error: %q", got.LastError)
	}
}

// Migration 066 left the ids a node minted as they were, so an AX placed
// before the rename kept a watcher under ax-glyph-, which the canvas no
// longer names: it could not find it, show it, or switch it off, and opening
// the AX wrote a second watcher beside it. Started, the engine moves each to
// the id the canvas names, and drops it where the canvas already wrote one.
func TestStartMovesAGlyphWatcherToTheIDTheCanvasNames(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	held := []*storage.Watcher{
		// Only under the old id: moved, and a tell like every AX.
		{ID: "ax-glyph-ax-9", Name: "AX Glyph: is", ActionType: storage.ActionTypePython,
			MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()},
		// Under both: the one the canvas wrote stays, the old one goes.
		{ID: "ax-glyph-ax-8", Name: "old", ActionType: storage.ActionTypePython,
			MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()},
		{ID: "ax-element-ax-8", Name: "new", ActionType: storage.ActionTypeTell,
			MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()},
		{ID: "se-glyph-se-7", Name: "SE", SemanticQuery: "science", SemanticThreshold: 0.4,
			ActionType: storage.ActionTypeSemanticMatch, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()},
	}
	for _, w := range held {
		if err := store.Create(ctx, w); err != nil {
			t.Fatalf("Create %s failed: %v", w.ID, err)
		}
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	q := engine.GetQueueStore()
	if err := q.Enqueue(&watcher.QueueEntry{WatcherID: "ax-glyph-ax-8", AttestationJSON: `{"id":"AS-G"}`,
		Reason: "paused", Attempt: 1, NotBefore: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	for _, gone := range []string{"ax-glyph-ax-9", "ax-glyph-ax-8", "se-glyph-se-7"} {
		if _, err := store.Get(ctx, gone); err == nil {
			t.Errorf("%s is still held", gone)
		}
		if n := queuedFor(t, q, gone); n != 0 {
			t.Errorf("%s still has %d queued", gone, n)
		}
	}

	moved, err := store.Get(ctx, "ax-element-ax-9")
	if err != nil {
		t.Fatalf("ax-element-ax-9 was not written: %v", err)
	}
	if moved.ActionType != storage.ActionTypeTell {
		t.Errorf("the moved AX watcher is %q, want tell", moved.ActionType)
	}

	kept, err := store.Get(ctx, "ax-element-ax-8")
	if err != nil {
		t.Fatalf("ax-element-ax-8 is gone: %v", err)
	}
	if kept.Name != "new" {
		t.Errorf("the watcher the canvas wrote was replaced by the old one: %q", kept.Name)
	}

	se, err := store.Get(ctx, "se-element-se-7")
	if err != nil {
		t.Fatalf("se-element-se-7 was not written: %v", err)
	}
	if se.SemanticQuery != "science" || se.ActionType != storage.ActionTypeSemanticMatch {
		t.Errorf("the moved SE watcher lost what it was: %q %q", se.SemanticQuery, se.ActionType)
	}
}

// One AX watcher the store will not write again holds back none of the others.
func TestOneWatcherThatWillNotSettleHoldsBackNoneOfTheOthers(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	stuck := &storage.Watcher{ID: "ax-element-ax-a-stuck", Name: "AX stuck",
		ActionType: storage.ActionTypePython, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	next := &storage.Watcher{ID: "ax-element-ax-z-next", Name: "AX next",
		ActionType: storage.ActionTypePython, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	// Newest is listed first: the stuck one is written last, so it is reached first.
	for _, w := range []*storage.Watcher{next, stuck} {
		if err := store.Create(ctx, w); err != nil {
			t.Fatalf("Create %s failed: %v", w.ID, err)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	if _, err := db.Exec(`CREATE TRIGGER refuse_stuck BEFORE UPDATE ON watchers
		WHEN OLD.id = 'ax-element-ax-a-stuck' BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatalf("refusing the stuck watcher failed: %v", err)
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	q := engine.GetQueueStore()
	for _, id := range []string{stuck.ID, next.ID} {
		if err := q.Enqueue(&watcher.QueueEntry{WatcherID: id, AttestationJSON: `{"id":"AS-S"}`,
			Reason: "paused", Attempt: 1, NotBefore: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("Enqueue for %s failed: %v", id, err)
		}
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	got, err := store.Get(ctx, next.ID)
	if err != nil {
		t.Fatalf("Get %s failed: %v", next.ID, err)
	}
	if got.ActionType != storage.ActionTypeTell {
		t.Errorf("the watcher after the stuck one is %q, want tell", got.ActionType)
	}
	if n := queuedFor(t, q, next.ID); n != 0 {
		t.Errorf("the watcher after the stuck one still has %d queued", n)
	}
	got, err = store.Get(ctx, stuck.ID)
	if err != nil {
		t.Fatalf("Get %s failed: %v", stuck.ID, err)
	}
	if got.ActionType != storage.ActionTypePython {
		t.Errorf("the stuck watcher is %q, want it left as it was", got.ActionType)
	}
}
