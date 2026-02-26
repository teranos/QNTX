package watcher_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// createTestWatchers inserts watcher rows so the FK constraint is satisfied.
func createTestWatchers(t *testing.T, db *sql.DB, ids ...string) {
	t.Helper()
	store := storage.NewWatcherStore(db)
	for _, id := range ids {
		err := store.Create(context.Background(), &storage.Watcher{
			ID:                id,
			Name:              id,
			ActionType:        storage.ActionTypeWebhook,
			ActionData:        "http://localhost",
			MaxFiresPerSecond: 60,
			Enabled:           true,
		})
		if err != nil {
			t.Fatalf("createTestWatchers(%s): %v", id, err)
		}
	}
}

func TestQueueStore_EnqueueAndDequeue(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1", "w2")
	store := watcher.NewQueueStore(db)

	// Enqueue two entries for different watchers
	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{"id":"as1"}`,
		Reason:          "rate_limited",
		Attempt:         0,
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue w1: %v", err)
	}

	err = store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w2",
		AttestationJSON: `{"id":"as2"}`,
		Reason:          "retry",
		Attempt:         1,
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue w2: %v", err)
	}

	// Dequeue should return one entry per watcher (round-robin)
	entries, err := store.DequeueRoundRobin(time.Now(), 50)
	if err != nil {
		t.Fatalf("DequeueRoundRobin: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Both should be 'running'
	for _, e := range entries {
		if e.Status != "running" {
			t.Errorf("Expected status 'running', got %q for entry %d", e.Status, e.ID)
		}
	}
}

func TestQueueStore_DequeueRespectsNotBefore(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1")
	store := watcher.NewQueueStore(db)

	// Enqueue with not_before in the future
	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{"id":"future"}`,
		Reason:          "retry",
		Attempt:         1,
		NotBefore:       time.Now().Add(1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dequeue now should return nothing
	entries, err := store.DequeueRoundRobin(time.Now(), 50)
	if err != nil {
		t.Fatalf("DequeueRoundRobin: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries (future not_before), got %d", len(entries))
	}
}

func TestQueueStore_RoundRobinFairness(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1", "w2")
	store := watcher.NewQueueStore(db)

	// Enqueue 3 entries for w1, 1 entry for w2
	for i := 0; i < 3; i++ {
		err := store.Enqueue(&watcher.QueueEntry{
			WatcherID:       "w1",
			AttestationJSON: `{"id":"as1"}`,
			Reason:          "rate_limited",
			NotBefore:       time.Now().Add(-1 * time.Second),
		})
		if err != nil {
			t.Fatalf("Enqueue w1[%d]: %v", i, err)
		}
	}
	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w2",
		AttestationJSON: `{"id":"as2"}`,
		Reason:          "rate_limited",
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue w2: %v", err)
	}

	// First dequeue: should get 1 from w1, 1 from w2 (round-robin)
	entries, err := store.DequeueRoundRobin(time.Now(), 50)
	if err != nil {
		t.Fatalf("DequeueRoundRobin: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries (one per watcher), got %d", len(entries))
	}

	// Complete both
	for _, e := range entries {
		store.Complete(e.ID)
	}

	// Second dequeue: should get 1 more from w1
	entries, err = store.DequeueRoundRobin(time.Now(), 50)
	if err != nil {
		t.Fatalf("DequeueRoundRobin round 2: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].WatcherID != "w1" {
		t.Errorf("Expected w1, got %s", entries[0].WatcherID)
	}
}

func TestQueueStore_CompleteAndFail(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1", "w2")
	store := watcher.NewQueueStore(db)

	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{"id":"as1"}`,
		Reason:          "rate_limited",
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entries, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	// Complete it
	if err := store.Complete(entries[0].ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Should not appear in next dequeue
	entries2, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries2) != 0 {
		t.Errorf("Expected 0 entries after complete, got %d", len(entries2))
	}

	// Test Fail
	err = store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w2",
		AttestationJSON: `{"id":"as2"}`,
		Reason:          "retry",
		Attempt:         1,
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue w2: %v", err)
	}

	entries3, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries3) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries3))
	}

	if err := store.Fail(entries3[0].ID, "test error"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	// Failed entry should not appear in dequeue
	entries4, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries4) != 0 {
		t.Errorf("Expected 0 entries after fail, got %d", len(entries4))
	}
}

func TestQueueStore_RequeueOrphans(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1")
	store := watcher.NewQueueStore(db)

	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{"id":"orphan"}`,
		Reason:          "retry",
		Attempt:         2,
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dequeue to make it 'running'
	entries, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	// Simulate crash: requeue orphans
	count, err := store.RequeueOrphans()
	if err != nil {
		t.Fatalf("RequeueOrphans: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 orphan requeued, got %d", count)
	}

	// Should be available for dequeue again
	entries2, _ := store.DequeueRoundRobin(time.Now(), 50)
	if len(entries2) != 1 {
		t.Errorf("Expected 1 entry after requeue, got %d", len(entries2))
	}
}

func TestQueueStore_Stats(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1", "w2")
	store := watcher.NewQueueStore(db)

	// Empty queue
	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalQueued != 0 {
		t.Errorf("Expected 0 total, got %d", stats.TotalQueued)
	}

	// Add entries
	for _, wid := range []string{"w1", "w1", "w2"} {
		store.Enqueue(&watcher.QueueEntry{
			WatcherID:       wid,
			AttestationJSON: `{}`,
			Reason:          "rate_limited",
			NotBefore:       time.Now().Add(-1 * time.Second),
		})
	}

	stats, err = store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalQueued != 3 {
		t.Errorf("Expected 3 total, got %d", stats.TotalQueued)
	}
	if stats.PerWatcher["w1"] != 2 {
		t.Errorf("Expected 2 for w1, got %d", stats.PerWatcher["w1"])
	}
	if stats.PerWatcher["w2"] != 1 {
		t.Errorf("Expected 1 for w2, got %d", stats.PerWatcher["w2"])
	}
}

func TestQueueStore_PurgeCompleted(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1")
	store := watcher.NewQueueStore(db)

	err := store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{}`,
		Reason:          "rate_limited",
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	entries, _ := store.DequeueRoundRobin(time.Now(), 50)
	store.Complete(entries[0].ID)

	// Purge with 0 retention should delete it
	purged, err := store.PurgeCompleted(0)
	if err != nil {
		t.Fatalf("PurgeCompleted: %v", err)
	}
	if purged != 1 {
		t.Errorf("Expected 1 purged, got %d", purged)
	}
}

func TestQueueStore_PurgeForWatcher(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	createTestWatchers(t, db, "w1", "w2")
	store := watcher.NewQueueStore(db)

	// Enqueue for w1 and w2
	store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w1",
		AttestationJSON: `{}`,
		Reason:          "rate_limited",
		NotBefore:       time.Now().Add(-1 * time.Second),
	})
	store.Enqueue(&watcher.QueueEntry{
		WatcherID:       "w2",
		AttestationJSON: `{}`,
		Reason:          "rate_limited",
		NotBefore:       time.Now().Add(-1 * time.Second),
	})

	// Purge only w1
	purged, err := store.PurgeForWatcher("w1")
	if err != nil {
		t.Fatalf("PurgeForWatcher: %v", err)
	}
	if purged != 1 {
		t.Errorf("Expected 1 purged, got %d", purged)
	}

	// w2 should still be queued
	stats, _ := store.Stats()
	if stats.TotalQueued != 1 {
		t.Errorf("Expected 1 remaining, got %d", stats.TotalQueued)
	}
	if stats.PerWatcher["w2"] != 1 {
		t.Errorf("Expected w2 to have 1, got %d", stats.PerWatcher["w2"])
	}
}
