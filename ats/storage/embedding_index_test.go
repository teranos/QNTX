package storage

import (
	"database/sql"
	"math"
	"sort"
	"sync"

	"go.uber.org/zap"
)

// memoryIndex is a VectorIndex held in memory, for the store's tests: what a
// vector search provider does, by cosine distance.
type memoryIndex struct {
	mu      sync.Mutex
	byModel map[string]map[string][]float32
}

func (m *memoryIndex) Keep(model string, _ int, id string, vector []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byModel == nil {
		m.byModel = map[string]map[string][]float32{}
	}
	if m.byModel[model] == nil {
		m.byModel[model] = map[string][]float32{}
	}
	m.byModel[model][id] = vector
	return nil
}

func (m *memoryIndex) Forget(model string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		delete(m.byModel[model], id)
	}
	return nil
}

func (m *memoryIndex) Nearest(model string, query []float32, limit int) ([]VectorHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var hits []VectorHit
	for held, vectors := range m.byModel {
		if model != "" && held != model {
			continue
		}
		for id, vector := range vectors {
			hits = append(hits, VectorHit{ID: id, Distance: 1 - cosine(query, vector)})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Distance < hits[j].Distance })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		if i >= len(b) {
			break
		}
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// indexedStore is an embedding store with its vectors in memory.
func indexedStore(db *sql.DB, logger *zap.Logger) *EmbeddingStore {
	store := NewEmbeddingStore(db, logger)
	store.SetIndex(&memoryIndex{})
	return store
}
