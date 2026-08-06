//go:build qntxwasm

// The drain loop's queue bookkeeping used to discard its errors. A refused
// write leaves the entry 'running' until the next restart requeues orphans,
// and nothing anywhere says so.
package watcher_test

import (
	"context"
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

func TestDrainReportsRefusedQueueWrite(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	fired := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case fired <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)

	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "drain-error-test",
		Name:              "Drain error test",
		ActionType:        storage.ActionTypeWebhook,
		ActionData:        target.URL + "/webhook",
		MaxFiresPerSecond: 100,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"drain-error"}},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Refuse exactly the write that marks an entry done. Dequeue's own
	// 'running' update still goes through, so the drain reaches Complete.
	if _, err := db.Exec(`CREATE TRIGGER refuse_complete
		BEFORE UPDATE ON watcher_execution_queue
		WHEN NEW.status = 'completed'
		BEGIN SELECT RAISE(ABORT, 'queue write refused'); END`); err != nil {
		t.Fatalf("install trigger failed: %v", err)
	}

	entry := &watcher.QueueEntry{
		WatcherID:       "drain-error-test",
		AttestationJSON: `{"id":"AS-DRAIN-ERROR","subjects":["s"],"predicates":["drain-error"]}`,
		Reason:          "retry",
		Attempt:         0,
		NotBefore:       time.Now().Add(-time.Second),
	}
	if err := engine.GetQueueStore().Enqueue(entry); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("drain never executed the queued entry")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.Get(context.Background(), "drain-error-test")
		if err != nil {
			t.Fatalf("Get watcher failed: %v", err)
		}
		if got.ErrorCount > 0 {
			if !contains(got.LastError, "queue") {
				t.Fatalf("watcher error does not name the queue: %q", got.LastError)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the refused queue write was swallowed: watcher records no error")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
