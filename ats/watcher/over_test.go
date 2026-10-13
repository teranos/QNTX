//go:build qntxwasm

package watcher_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// "over means longer than": a watcher asking `over` measures each attestation
// as it arrives, between the start and end its type names.
func TestAWatcherMeasuresOverAsAttestationsArrive(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", zap.NewNop().Sugar())
	ctx := context.Background()

	insert := func(id string, subjects, predicates []string, attributes map[string]any) {
		t.Helper()
		row := func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal %v: %v", v, err)
			}
			return string(b)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, row(subjects), row(predicates), row([]string{}), row([]string{"hr"}),
			time.Now().Format(time.RFC3339), "test", row(attributes)); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("type-employed", []string{"employed"}, []string{"type"},
		map[string]any{"start_field": "from", "end_field": "until"})
	insert("six-years", []string{"ALICE"}, []string{"employed"},
		map[string]any{"from": "2018-03-01", "until": "2024-03-01"})
	insert("two-years", []string{"ALICE"}, []string{"employed"},
		map[string]any{"from": "2022-03-01", "until": "2024-03-01"})
	if err := qntxtest.SyncJunctionTables(db); err != nil {
		t.Fatalf("sync junction tables: %v", err)
	}

	if err := storage.NewWatcherStore(db).Create(ctx, &storage.Watcher{
		Filter:            types.AxFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded()},
		ID:                "long-employment",
		Name:              "Long employment",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerSecond: 105,
		Enabled:           true,
		AxQuery:           "ALICE is employed over 5y",
	}); err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer engine.Stop()
	if err := engine.GetParseError("long-employment"); err != nil {
		t.Fatalf("a watcher asking over is refused: %v", err)
	}

	var matched []string
	engine.SetBroadcastCallback(func(watcherID string, as *types.As, score float32) {
		matched = append(matched, as.ID)
	})

	for _, as := range []*types.As{
		{ID: "arrives-six", Subjects: []string{"ALICE"}, Predicates: []string{"employed"}, Actors: []string{"hr"},
			Timestamp: time.Now(), Attributes: map[string]any{"from": "2018-03-01", "until": "2024-03-01"}},
		{ID: "arrives-two", Subjects: []string{"ALICE"}, Predicates: []string{"employed"}, Actors: []string{"hr"},
			Timestamp: time.Now(), Attributes: map[string]any{"from": "2022-03-01", "until": "2024-03-01"}},
	} {
		engine.OnAttestationCreated(as)
	}
	if got := matched; len(got) != 1 || got[0] != "arrives-six" {
		t.Fatalf("arriving: matched %v, want only arrives-six", got)
	}

	matched = nil
	if err := engine.QueryHistoricalMatches("long-employment"); err != nil {
		t.Fatalf("historical: %v", err)
	}
	if got := matched; len(got) != 1 || got[0] != "six-years" {
		t.Fatalf("historical: matched %v, want only six-years", got)
	}
}
