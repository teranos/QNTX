// +build integration

package watcher_test

import (
	"net/http"
	"net/http/httptest"
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
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{}, // Match all
	}
	if err := store.Create(w); err != nil {
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
	w, err := store.Get("retry-test")
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