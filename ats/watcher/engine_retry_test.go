//go:build integration
// +build integration

package watcher_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

func TestEngine_RetryLogic(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	// Mock endpoint that fails initially then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle Python execution endpoint
		if r.URL.Path == "/api/python/execute" {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("temporary error"))
			} else {
				w.WriteHeader(http.StatusOK)
			}
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, server.URL, logger)

	// Create watcher
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "retry-test",
		Name:              "Retry Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerSecond: 105,
		Enabled:           true,
		Filter:            types.AxFilter{}, // Match all
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Trigger attestation
	engine.OnAttestationCreated(&types.As{
		ID:         "retry-attestation",
		Subjects:   []string{"test"},
		Predicates: []string{"retry"},
	})

	// Wait for retries (initial + 2 retries with exponential backoff: 1s, 2s)
	// Add extra buffer for processing time and ticker intervals
	// Retry ticker runs every 1s, so we need to wait for multiple ticks
	time.Sleep(6 * time.Second)

	// Should have succeeded on third attempt
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	// Check that success was recorded
	w, err := store.Get(context.Background(), "retry-test")
	if err != nil {
		t.Fatalf("Failed to get watcher: %v", err)
	}
	if w.FireCount != 1 {
		t.Errorf("Expected FireCount=1, got %d", w.FireCount)
	}
	if w.ErrorCount < 2 {
		t.Errorf("Expected ErrorCount>=2, got %d", w.ErrorCount)
	}
}

// TestEngine_RateLimitDrain verifies the critical path:
// rate-limited attestations are enqueued, then drained and executed
// at the rate limiter's pace via Reserve/Cancel timing.
func TestEngine_RateLimitDrain(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	var execCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/python/execute" {
			execCount.Add(1)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, server.URL, logger)

	// 2 fires per second — one token every 500ms
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "drain-test",
		Name:              "Drain Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerSecond: 2,
		Enabled:           true,
		Filter:            types.AxFilter{},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Fire 5 attestations rapidly — 1 should execute immediately, 4 should be queued
	for i := 0; i < 5; i++ {
		engine.OnAttestationCreated(&types.As{
			ID:         fmt.Sprintf("drain-%d", i),
			Subjects:   []string{"test"},
			Predicates: []string{"drain"},
		})
	}

	// Let the immediate execution land
	time.Sleep(200 * time.Millisecond)

	// Verify queue has entries (rate-limited attestations were enqueued, not dropped)
	stats, err := engine.GetQueueStore().Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalQueued == 0 {
		t.Fatal("Expected queued entries after rapid fire, got 0 — rate-limited attestations were dropped")
	}

	immediate := execCount.Load()
	if immediate < 1 {
		t.Errorf("Expected at least 1 immediate execution, got %d", immediate)
	}

	// Wait for drain loop to process all queued entries
	// 4 queued entries at 2/sec = ~2 seconds, add buffer for drain interval jitter
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for drain: got %d executions, want 5", execCount.Load())
		case <-time.After(200 * time.Millisecond):
			if execCount.Load() >= 5 {
				goto done
			}
		}
	}
done:

	// Verify all 5 executed
	final := execCount.Load()
	if final != 5 {
		t.Errorf("Expected 5 total executions, got %d", final)
	}

	// Verify queue is drained
	stats, err = engine.GetQueueStore().Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalQueued > 0 {
		t.Errorf("Expected empty queue after drain, got %d queued", stats.TotalQueued)
	}

	// Verify fire count was recorded
	w, err = store.Get(context.Background(), "drain-test")
	if err != nil {
		t.Fatalf("Get watcher failed: %v", err)
	}
	if w.FireCount != 5 {
		t.Errorf("Expected FireCount=5, got %d", w.FireCount)
	}
}
