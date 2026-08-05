//go:build qntxwasm

// The engine's read path is a backend seam, and it was cut in SQL — a seam
// only SQLite fits. These tests hold it to a filter, which is what a watcher
// has in the first place.
package watcher_test

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// filterOnlyBackend has the shape duckdbcgo.DuckdbStore has: RawAttestationStore
// plus QueryableStore. It cannot answer SQL, which is the point.
type filterOnlyBackend struct {
	held []*types.As
}

func (b *filterOnlyBackend) CreateAttestation(as *types.As) error {
	b.held = append(b.held, as)
	return nil
}

func (b *filterOnlyBackend) GetAttestation(id string) (*types.As, error) {
	for _, as := range b.held {
		if as.ID == id {
			return as, nil
		}
	}
	return nil, nil
}

func (b *filterOnlyBackend) AttestationExists(id string) bool {
	as, _ := b.GetAttestation(id)
	return as != nil
}

func (b *filterOnlyBackend) CountAttestations() (int, error) { return len(b.held), nil }

func (b *filterOnlyBackend) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	var out []*types.As
	for _, as := range b.held {
		if matchesAny(as.Predicates, filter.Predicates) {
			out = append(out, as)
		}
	}
	return out, nil
}

func matchesAny(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, h := range have {
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}

// TestParquetStoreIsAnAttestationReader holds the assertion server/watcher_handlers.go
// makes. When it fails, the parquet server builds its engine with a nil reader.
func TestParquetStoreIsAnAttestationReader(t *testing.T) {
	var store ats.AttestationStore = storage.NewAtsStore(&filterOnlyBackend{}, zap.NewNop().Sugar())

	if _, ok := store.(watcher.AttestationReader); !ok {
		t.Fatal("AtsStore does not satisfy watcher.AttestationReader; " +
			"the parquet server hands the engine a nil reader")
	}
}

// TestHistoricalMatchesReadThroughTheBackend proves the read reaches the backend
// that holds attestations, rather than compiling and returning nothing.
func TestHistoricalMatchesReadThroughTheBackend(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	backend := &filterOnlyBackend{held: []*types.As{{
		ID:         "AS-ADSCAMPA-ADSDECLA-CAPY-TEST",
		Subjects:   []string{"ads:campaign:test"},
		Predicates: []string{"ads:declared"},
		Contexts:   []string{"capy"},
	}}}
	store := storage.NewAtsStore(backend, logger)

	reader, ok := any(store).(watcher.AttestationReader)
	if !ok {
		t.Fatal("AtsStore does not satisfy watcher.AttestationReader")
	}

	engine := watcher.NewEngine(db, reader, "http://localhost:8770", logger)

	var broadcast []string
	engine.SetBroadcastCallback(func(_ string, as *types.As, _ float32) {
		broadcast = append(broadcast, as.ID)
	})

	w := &storage.Watcher{
		ID:                "crier-observe",
		Name:              "Crier observes declarations",
		ActionType:        storage.ActionTypeWebhook,
		ActionData:        "http://127.0.0.1:1/never-called",
		MaxFiresPerSecond: 1,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"ads:declared"}},
	}
	if err := storage.NewWatcherStore(db).Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	if err := engine.QueryHistoricalMatches("crier-observe"); err != nil {
		t.Fatalf("QueryHistoricalMatches failed: %v", err)
	}

	if len(broadcast) != 1 || broadcast[0] != "AS-ADSCAMPA-ADSDECLA-CAPY-TEST" {
		t.Fatalf("historical query did not read through the backend: broadcast %v", broadcast)
	}
}
