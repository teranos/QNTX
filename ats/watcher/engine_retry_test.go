//go:build integration
// +build integration

package watcher_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/errors"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// retryPythonExecutor fails the first N calls then succeeds.
type retryPythonExecutor struct {
	mu       sync.Mutex
	attempts int
	failUntil int
}

func (m *retryPythonExecutor) Execute(_ context.Context, _ string, _ string, _ []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts++
	if m.attempts < m.failUntil {
		return nil, errors.New("temporary error")
	}
	return nil, nil
}

func (m *retryPythonExecutor) getAttempts() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attempts
}

// countingPythonExecutor counts successful executions.
type countingPythonExecutor struct {
	count atomic.Int32
}

func (m *countingPythonExecutor) Execute(_ context.Context, _ string, _ string, _ []byte) ([]byte, error) {
	m.count.Add(1)
	return nil, nil
}

func TestEngine_RetryLogic(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	mock := &retryPythonExecutor{failUntil: 3}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", logger)
	engine.SetPythonExecutor(mock)

	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "retry-test",
		Name:              "Retry Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerSecond: 105,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"retry"}},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	engine.OnAttestationCreated(&types.As{
		ID:         "retry-attestation",
		Subjects:   []string{"test"},
		Predicates: []string{"retry"},
	})

	// Wait for retries (initial + 2 retries with exponential backoff: 1s, 2s)
	time.Sleep(10 * time.Second)

	if got := mock.getAttempts(); got != 3 {
		t.Errorf("Expected 3 attempts, got %d", got)
	}

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

func TestEngine_RateLimitDrain(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	mock := &countingPythonExecutor{}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", logger)
	engine.SetPythonExecutor(mock)

	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "drain-test",
		Name:              "Drain Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerSecond: 2,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"drain"}},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	for i := 0; i < 5; i++ {
		engine.OnAttestationCreated(&types.As{
			ID:         fmt.Sprintf("drain-%d", i),
			Subjects:   []string{"test"},
			Predicates: []string{"drain"},
		})
	}

	// Let the immediate execution land
	time.Sleep(200 * time.Millisecond)

	stats, err := engine.GetQueueStore().Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalQueued == 0 {
		t.Fatal("Expected queued entries after rapid fire, got 0")
	}

	immediate := mock.count.Load()
	if immediate < 1 {
		t.Errorf("Expected at least 1 immediate execution, got %d", immediate)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("Timed out waiting for drain: got %d executions, want 5", mock.count.Load())
		case <-time.After(200 * time.Millisecond):
			if mock.count.Load() >= 5 {
				goto done
			}
		}
	}
done:

	final := mock.count.Load()
	if final != 5 {
		t.Errorf("Expected 5 total executions, got %d", final)
	}

	stats, err = engine.GetQueueStore().Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalQueued > 0 {
		t.Errorf("Expected empty queue after drain, got %d queued", stats.TotalQueued)
	}

	w, err = store.Get(context.Background(), "drain-test")
	if err != nil {
		t.Fatalf("Get watcher failed: %v", err)
	}
	if w.FireCount != 5 {
		t.Errorf("Expected FireCount=5, got %d", w.FireCount)
	}
}
