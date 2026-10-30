package watcher_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// noPlugins has no plugin loaded, and is never asked to run one.
type noPlugins struct{}

func (noPlugins) ExecutePluginJob(context.Context, string, string, []byte) ([]byte, error) {
	panic("a plugin that is not loaded was asked to run")
}
func (noPlugins) IsPluginLoaded(string) bool { return false }

// A row waiting on a plugin was put back a minute later, every minute, with
// nothing counting how long: on a node that never loads the plugin it waited
// forever, and nothing said so. Past a day it is given up, and the watcher's
// error says which plugin it waited on.
func TestARowWaitingOnAPluginIsGivenUpAfterADay(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	w := &storage.Watcher{ID: "waits-on-reduce", Name: "waits on reduce",
		ActionType: storage.ActionTypePluginExecute, ActionData: `{"plugin_name":"reduce","handler_name":"h"}`,
		MaxFiresPerSecond: 10, Enabled: true, Filter: unbounded()}
	if err := store.Create(ctx, w); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	engine.SetPluginExecutor(noPlugins{})
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	q := engine.GetQueueStore()
	for _, id := range []string{"AS-OLD", "AS-NEW"} {
		if err := q.Enqueue(&watcher.QueueEntry{WatcherID: w.ID, AttestationJSON: `{"id":"` + id + `"}`,
			Reason: "paused", Attempt: 1, NotBefore: time.Now().Add(-time.Second)}); err != nil {
			t.Fatalf("Enqueue %s failed: %v", id, err)
		}
	}
	dayAgo := time.Now().Add(-25 * time.Hour).UTC().Format(sqlTimeFormat)
	if _, err := db.Exec(`UPDATE watcher_execution_queue SET created_at = ? WHERE attestation_json LIKE '%AS-OLD%'`, dayAgo); err != nil {
		t.Fatalf("ageing the row failed: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		var old, fresh string
		if err := db.QueryRow(`SELECT
			(SELECT status FROM watcher_execution_queue WHERE attestation_json LIKE '%AS-OLD%'),
			(SELECT status FROM watcher_execution_queue WHERE attestation_json LIKE '%AS-NEW%')`).Scan(&old, &fresh); err != nil {
			t.Fatalf("reading the queue failed: %v", err)
		}
		if old == "failed" {
			if fresh != "queued" {
				t.Errorf("a row waiting under a day is %q, want queued", fresh)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a row waiting a day on a plugin is still %q", old)
		}
		time.Sleep(100 * time.Millisecond)
	}

	got, err := store.Get(ctx, w.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ErrorCount != 1 || !strings.Contains(got.LastError, "reduce") {
		t.Errorf("giving up was not the watcher's error: %d errors, last %q", got.ErrorCount, got.LastError)
	}
}
