//go:build qntxwasm

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

// sameVector embeds everything as one vector, so every query matches.
type sameVector struct{}

func (sameVector) GenerateEmbedding(string) ([]float32, error)       { return []float32{1, 0}, nil }
func (sameVector) ComputeSimilarity(_, _ []float32) (float32, error) { return 1, nil }
func (sameVector) SerializeEmbedding([]float32) ([]byte, error)      { return nil, nil }

// A semantic element only tells. Past its rate each match was queued, to be
// marked done later having run nothing.
func TestASemanticElementPastItsRateQueuesNothing(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	store := storage.NewWatcherStore(db)

	w := &storage.Watcher{ID: "se-element-se-1", Name: "SE", SemanticQuery: "anything", SemanticThreshold: 0.5,
		ActionType: storage.ActionTypeSemanticMatch, MaxFiresPerSecond: 1, Enabled: true, Filter: unbounded()}
	if err := store.Create(ctx, w); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://unused", zap.NewNop().Sugar())
	engine.SetEmbeddingService(sameVector{})
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	told := 0
	engine.SetBroadcastCallback(func(string, *types.As, float32) { told++ })
	for i := 0; i < 5; i++ {
		as := &types.As{ID: "AS-SE-" + string(rune('a'+i)), Subjects: []string{"s"}, Predicates: []string{"settle"},
			Contexts: []string{"c"}, Actors: []string{"a"}, Timestamp: time.Now()}
		engine.OnAttestationEmbedded(as, []float32{1, 0})
	}

	if told != 5 {
		t.Fatalf("the page was told of %d matches, want 5", told)
	}
	if n := queuedFor(t, engine.GetQueueStore(), w.ID); n != 0 {
		t.Errorf("a semantic element queued %d matches", n)
	}
}
