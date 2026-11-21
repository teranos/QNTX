package storage

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	errors "github.com/teranos/sacred-error"
)

// blobOf is a vector as the store keeps it.
func blobOf(v ...float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func kept(t *testing.T, store *EmbeddingStore, id string, cluster int, v ...float32) {
	t.Helper()
	err := store.Save(&EmbeddingModel{ID: id, SourceType: "attestation", SourceID: id, Text: id, Embedding: blobOf(v...), Model: "m", Dimensions: len(v)})
	var notIndexed NotIndexed
	if err != nil && !errors.As(err, &notIndexed) {
		require.NoError(t, err)
	}
	if cluster >= 0 {
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{{ID: id, ClusterID: cluster, Probability: 1}}))
	}
}

// A search answers limit live matches when hits nearer than them are of
// another cluster, or have no row: the index is asked for more.
func TestASearchHoldsLimitResultsPastHitsThatAreNone(t *testing.T) {
	store := indexedStore(qntxtest.CreateTestDB(t), zap.NewNop())
	for i, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		kept(t, store, id, 1, 1, float32(i)*0.01)
	}
	kept(t, store, "far", 2, 1, 0.5)

	cluster := 2
	results, err := store.SemanticSearch(blobOf(1, 0), 1, 0, &cluster, "m")
	require.NoError(t, err)
	require.Len(t, results, 1, "twelve nearer hits of cluster 1 do not hide cluster 2's")
	assert.Equal(t, "far", results[0].EmbeddingID)

	// A row swept with its vector left indexed is no result, and the next
	// nearest takes its place.
	_, err = store.db.Exec(`DELETE FROM embeddings WHERE id = 'a'`)
	require.NoError(t, err)
	results, err = store.SemanticSearch(blobOf(1, 0), 2, 0, nil, "m")
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, []string{"b", "c"}, []string{results[0].EmbeddingID, results[1].EmbeddingID})
}

// What was kept while no index was, is searchable once one is handed and the
// kept embeddings are offered to it.
func TestKeptEmbeddingsAreIndexedOnceAnIndexIs(t *testing.T) {
	store := NewEmbeddingStore(qntxtest.CreateTestDB(t), zap.NewNop())
	kept(t, store, "before", -1, 1, 0)

	store.SetIndex(&memoryIndex{})
	results, err := store.SemanticSearch(blobOf(1, 0), 5, 0, nil, "m")
	require.NoError(t, err)
	assert.Empty(t, results, "nothing was offered yet")

	indexed, err := store.IndexKept()
	require.NoError(t, err)
	assert.Equal(t, 1, indexed)
	results, err = store.SemanticSearch(blobOf(1, 0), 5, 0, nil, "m")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "before", results[0].EmbeddingID)
}

// refusingIndex is an index that takes nothing out until it is let.
type refusingIndex struct {
	memoryIndex
	refuses bool
}

func (r *refusingIndex) Forget(model string, ids []string) error {
	if r.refuses {
		return errors.New("the provider is down")
	}
	return r.memoryIndex.Forget(model, ids)
}

// A vector the index did not take out when its row was deleted is recorded,
// and taken out when the index is asked again.
func TestAVectorNotTakenOutIsAskedAgain(t *testing.T) {
	index := &refusingIndex{refuses: true}
	store := NewEmbeddingStore(qntxtest.CreateTestDB(t), zap.NewNop())
	store.SetIndex(index)
	kept(t, store, "gone", -1, 1, 0)

	err := store.DeleteBySource("attestation", "gone")
	var notIndexed NotIndexed
	require.True(t, errors.As(err, &notIndexed), "the row is deleted and its vector is not taken out: %v", err)
	pending, err := store.pendingForgets()
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"m": {"gone"}}, pending)

	index.refuses = false
	forgotten, err := store.ForgetPending()
	require.NoError(t, err)
	assert.Equal(t, 1, forgotten)
	pending, err = store.pendingForgets()
	require.NoError(t, err)
	assert.Empty(t, pending)
	hits, err := index.Nearest("m", []float32{1, 0}, 5)
	require.NoError(t, err)
	assert.Empty(t, hits)
}

// A delete or a sweep that finds nothing to delete still asks again for what
// is recorded to be taken out.
func TestAVectorNotTakenOutIsAskedAgainByADeleteOrSweepOfNothing(t *testing.T) {
	for _, again := range map[string]func(*EmbeddingStore) error{
		"delete": func(s *EmbeddingStore) error { return s.DeleteBySource("attestation", "never") },
		"sweep":  func(s *EmbeddingStore) error { _, err := s.SweepStaleEmbeddings(); return err },
	} {
		index := &refusingIndex{refuses: true}
		store := NewEmbeddingStore(qntxtest.CreateTestDB(t), zap.NewNop())
		store.SetIndex(index)
		kept(t, store, "gone", -1, 1, 0)
		require.Error(t, store.DeleteBySource("attestation", "gone"))

		index.refuses = false
		require.NoError(t, again(store))
		pending, err := store.pendingForgets()
		require.NoError(t, err)
		assert.Empty(t, pending)
	}
}

// A search whose hits are more than SQLite binds in one query reads their
// rows in batches.
func TestRowsAreReadPastSQLitesLimitOnBoundParameters(t *testing.T) {
	store := NewEmbeddingStore(qntxtest.CreateTestDB(t), zap.NewNop())
	kept(t, store, "held", -1, 1, 0)
	ids := make([]string, 40000)
	for i := range ids {
		ids[i] = "absent"
	}
	ids[len(ids)-1] = "held"
	rows, err := store.rowsByID(ids)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Contains(t, rows, "held")
}
