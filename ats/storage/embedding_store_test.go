package storage

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// TODO: Fix sqlite-vec integration
// These tests currently fail with "no such module: vec0" because the standard
// mattn/go-sqlite3 driver doesn't include sqlite-vec extension.
//
// To fix:
// 1. Replace mattn/go-sqlite3 with github.com/asg017/sqlite-vec-go-bindings/ncruces
// 2. Or build a custom SQLite with vec0 module included
// 3. Or make the vec_embeddings table creation conditional in migration 019
//
// See docs/embeddings_integration_status.md for details.

// Simple test ID generator
var testIDCounter = 0

func generateTestID() string {
	testIDCounter++
	return fmt.Sprintf("test-id-%d", testIDCounter)
}

// Helper function to create a test FLOAT32_BLOB embedding
func createTestEmbedding(dimensions int) []byte {
	embedding := make([]float32, dimensions)
	for i := range embedding {
		// Create a simple pattern for testing
		embedding[i] = float32(i) / float32(dimensions)
	}

	// Convert to FLOAT32_BLOB format (little-endian)
	buf := make([]byte, dimensions*4)
	for i, val := range embedding {
		bits := *(*uint32)(unsafe.Pointer(&val))
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

func TestEmbeddingStore_Save(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	embedding := &EmbeddingModel{
		SourceType: "attestation",
		SourceID:   generateTestID(),
		Text:       "test attestation content",
		Embedding:  createTestEmbedding(384),
		Model:      "all-MiniLM-L6-v2",
		Dimensions: 384,
	}

	err := store.Save(embedding)
	require.NoError(t, err)
	assert.NotEmpty(t, embedding.ID)
	assert.False(t, embedding.CreatedAt.IsZero())
	assert.False(t, embedding.UpdatedAt.IsZero())

	// Verify it was saved
	retrieved, err := store.GetBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, embedding.ID, retrieved.ID)
	assert.Equal(t, embedding.Text, retrieved.Text)
	assert.Equal(t, embedding.Model, retrieved.Model)
	assert.Equal(t, embedding.Dimensions, retrieved.Dimensions)
}

func TestEmbeddingStore_GetBySource_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	retrieved, err := store.GetBySource("attestation", "non-existent-id")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestEmbeddingStore_SemanticSearch(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create test embeddings with different similarities
	embeddings := []*EmbeddingModel{
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "cat kitten feline",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "dog puppy canine",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "car vehicle automobile",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
	}

	for _, emb := range embeddings {
		err := store.Save(emb)
		require.NoError(t, err)
	}

	// Search with a query embedding
	queryEmbedding := createTestEmbedding(384)
	results, err := store.SemanticSearch(queryEmbedding, 10, 0.0, nil)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Results should be ordered by distance (ascending)
	for i := 1; i < len(results); i++ {
		assert.LessOrEqual(t, results[i-1].Distance, results[i].Distance)
	}

	// Test with threshold
	results, err = store.SemanticSearch(queryEmbedding, 10, 0.9, nil)
	require.NoError(t, err)
	// Should filter out low similarity results
	for _, result := range results {
		assert.GreaterOrEqual(t, result.Similarity, float32(0.9))
	}
}

func TestEmbeddingStore_DeleteBySource(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	embedding := &EmbeddingModel{
		SourceType: "attestation",
		SourceID:   generateTestID(),
		Text:       "test content to delete",
		Embedding:  createTestEmbedding(384),
		Model:      "all-MiniLM-L6-v2",
		Dimensions: 384,
	}

	// Save the embedding
	err := store.Save(embedding)
	require.NoError(t, err)

	// Verify it exists
	retrieved, err := store.GetBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Delete it
	err = store.DeleteBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)

	// Verify it's gone
	retrieved, err = store.GetBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)
	assert.Nil(t, retrieved)

	// Delete non-existent should not error
	err = store.DeleteBySource("attestation", "non-existent")
	require.NoError(t, err)
}

func TestEmbeddingStore_BatchSaveAttestationEmbeddings(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	embeddings := []*EmbeddingModel{
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "batch test 1",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "batch test 2",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
		{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       "batch test 3",
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
	}

	err := store.BatchSaveAttestationEmbeddings(embeddings)
	require.NoError(t, err)

	// Verify all were saved
	for _, emb := range embeddings {
		retrieved, err := store.GetBySource(emb.SourceType, emb.SourceID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		assert.Equal(t, emb.Text, retrieved.Text)
	}

	// Verify they're searchable
	queryEmbedding := createTestEmbedding(384)
	results, err := store.SemanticSearch(queryEmbedding, 10, 0.0, nil)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestEmbeddingStore_EmptyBatch(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Empty batch should not error
	err := store.BatchSaveAttestationEmbeddings([]*EmbeddingModel{})
	require.NoError(t, err)
}

func TestEmbeddingStore_UpdateExisting(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	embedding := &EmbeddingModel{
		SourceType: "attestation",
		SourceID:   generateTestID(),
		Text:       "original text",
		Embedding:  createTestEmbedding(384),
		Model:      "all-MiniLM-L6-v2",
		Dimensions: 384,
	}

	// Save initial
	err := store.Save(embedding)
	require.NoError(t, err)
	originalID := embedding.ID

	// Small delay to ensure timestamps differ
	time.Sleep(10 * time.Millisecond)

	// Update with new text and embedding
	embedding.Text = "updated text"
	embedding.Embedding = createTestEmbedding(384) // Different embedding
	err = store.Save(embedding)
	require.NoError(t, err)

	// Verify update
	retrieved, err := store.GetBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)
	assert.Equal(t, originalID, retrieved.ID) // ID should remain the same
	assert.Equal(t, "updated text", retrieved.Text)
	// UpdatedAt should be at least as recent as CreatedAt
	assert.True(t, retrieved.UpdatedAt.After(retrieved.CreatedAt) || retrieved.UpdatedAt.Equal(retrieved.CreatedAt))
}

func TestEmbeddingStore_UpdateProjections(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Save 3 embeddings
	embeddings := make([]*EmbeddingModel, 3)
	for i := range embeddings {
		embeddings[i] = &EmbeddingModel{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       fmt.Sprintf("projection test %d", i),
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		}
		require.NoError(t, store.Save(embeddings[i]))
	}

	// Update projections for all 3 with method "umap"
	assignments := []ProjectionAssignment{
		{ID: embeddings[0].ID, X: 1.5, Y: -2.3},
		{ID: embeddings[1].ID, X: 0.0, Y: 4.7},
		{ID: embeddings[2].ID, X: -3.1, Y: 0.9},
	}
	err := store.UpdateProjections("umap", assignments)
	require.NoError(t, err)

	// GetProjectionsByMethod should return all 3 with correct coordinates
	projections, err := store.GetProjectionsByMethod("umap")
	require.NoError(t, err)
	assert.Len(t, projections, 3)

	// Build a map for easy lookup
	byID := make(map[string]ProjectionWithCluster)
	for _, p := range projections {
		byID[p.ID] = p
	}

	for _, a := range assignments {
		p, ok := byID[a.ID]
		require.True(t, ok, "projection missing for %s", a.ID)
		assert.Equal(t, a.X, p.X)
		assert.Equal(t, a.Y, p.Y)
		assert.Equal(t, "umap", p.Method)
	}
}

func TestEmbeddingStore_MultiMethodProjections(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Save 2 embeddings
	embeddings := make([]*EmbeddingModel, 2)
	for i := range embeddings {
		embeddings[i] = &EmbeddingModel{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       fmt.Sprintf("multi-method test %d", i),
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		}
		require.NoError(t, store.Save(embeddings[i]))
	}

	// Store projections for umap and pca
	umapAssignments := []ProjectionAssignment{
		{ID: embeddings[0].ID, X: 1.0, Y: 2.0},
		{ID: embeddings[1].ID, X: 3.0, Y: 4.0},
	}
	pcaAssignments := []ProjectionAssignment{
		{ID: embeddings[0].ID, X: -1.0, Y: -2.0},
		{ID: embeddings[1].ID, X: -3.0, Y: -4.0},
	}

	require.NoError(t, store.UpdateProjections("umap", umapAssignments))
	require.NoError(t, store.UpdateProjections("pca", pcaAssignments))

	// Each method returns its own coordinates
	umapProj, err := store.GetProjectionsByMethod("umap")
	require.NoError(t, err)
	assert.Len(t, umapProj, 2)
	umapXs := []float64{umapProj[0].X, umapProj[1].X}
	assert.ElementsMatch(t, []float64{1.0, 3.0}, umapXs)

	pcaProj, err := store.GetProjectionsByMethod("pca")
	require.NoError(t, err)
	assert.Len(t, pcaProj, 2)
	pcaXs := []float64{pcaProj[0].X, pcaProj[1].X}
	assert.ElementsMatch(t, []float64{-1.0, -3.0}, pcaXs)

	// Empty method returns nothing
	tsneProj, err := store.GetProjectionsByMethod("tsne")
	require.NoError(t, err)
	assert.Empty(t, tsneProj)

	// GetAllProjectionMethods returns stored methods
	methods, err := store.GetAllProjectionMethods()
	require.NoError(t, err)
	assert.Equal(t, []string{"pca", "umap"}, methods) // alphabetical
}

func TestEmbeddingStore_GetProjectionsByMethod_ExcludesUnprojected(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Save 3 embeddings
	embeddings := make([]*EmbeddingModel, 3)
	for i := range embeddings {
		embeddings[i] = &EmbeddingModel{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       fmt.Sprintf("exclude test %d", i),
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		}
		require.NoError(t, store.Save(embeddings[i]))
	}

	// Project only the first 2
	err := store.UpdateProjections("umap", []ProjectionAssignment{
		{ID: embeddings[0].ID, X: 1.0, Y: 2.0},
		{ID: embeddings[1].ID, X: 3.0, Y: 4.0},
	})
	require.NoError(t, err)

	projections, err := store.GetProjectionsByMethod("umap")
	require.NoError(t, err)
	assert.Len(t, projections, 2)

	// The unprojected embedding must not appear
	for _, p := range projections {
		assert.NotEqual(t, embeddings[2].ID, p.ID)
	}
}

func TestEmbeddingStore_UpdateProjections_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	err := store.UpdateProjections("umap", []ProjectionAssignment{})
	require.NoError(t, err)

	// No projections should exist
	projections, err := store.GetProjectionsByMethod("umap")
	require.NoError(t, err)
	assert.Empty(t, projections)
}

func TestEmbeddingStore_ProjectionRoundTrip(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	embedding := &EmbeddingModel{
		SourceType: "attestation",
		SourceID:   generateTestID(),
		Text:       "round trip test",
		Embedding:  createTestEmbedding(384),
		Model:      "all-MiniLM-L6-v2",
		Dimensions: 384,
	}
	require.NoError(t, store.Save(embedding))

	// Project it
	err := store.UpdateProjections("umap", []ProjectionAssignment{
		{ID: embedding.ID, X: -7.77, Y: 3.14},
	})
	require.NoError(t, err)

	// Retrieve via GetBySource — embedding still intact
	retrieved, err := store.GetBySource(embedding.SourceType, embedding.SourceID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, embedding.Text, retrieved.Text)

	// Retrieve projection via GetProjectionsByMethod — coordinates survive full cycle
	projections, err := store.GetProjectionsByMethod("umap")
	require.NoError(t, err)
	require.Len(t, projections, 1)
	assert.Equal(t, embedding.ID, projections[0].ID)
	assert.Equal(t, embedding.SourceID, projections[0].SourceID)
	assert.Equal(t, "umap", projections[0].Method)
	assert.Equal(t, -7.77, projections[0].X)
	assert.Equal(t, 3.14, projections[0].Y)
}

func TestEmbeddingStore_ClusterRunCRUD(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	run := &ClusterRun{
		ID:             "CR_test_001",
		NPoints:        100,
		NClusters:      5,
		NNoise:         10,
		MinClusterSize: 5,
		DurationMS:     42,
		CreatedAt:      time.Now().UTC(),
	}
	err := store.CreateClusterRun(run)
	require.NoError(t, err)

	// Duplicate insert should fail
	err = store.CreateClusterRun(run)
	assert.Error(t, err)
}

func TestEmbeddingStore_ClusterIdentityLifecycle(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create run first (foreign key)
	run := &ClusterRun{
		ID: "CR_lifecycle", NPoints: 10, NClusters: 2, NNoise: 1,
		MinClusterSize: 5, DurationMS: 10, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	// Create two clusters (SQLite INTEGER PRIMARY KEY auto-assigns starting at 1)
	id0, err := store.CreateCluster("CR_lifecycle")
	require.NoError(t, err)

	id1, err := store.CreateCluster("CR_lifecycle")
	require.NoError(t, err)
	assert.NotEqual(t, id0, id1, "cluster IDs must be unique")

	// Both should be active
	active, err := store.GetActiveClusterIdentities()
	require.NoError(t, err)
	assert.Len(t, active, 2)

	// Dissolve cluster 0
	require.NoError(t, store.DissolveCluster(id0, "CR_lifecycle"))

	// Only cluster 1 should be active
	active, err = store.GetActiveClusterIdentities()
	require.NoError(t, err)
	assert.Len(t, active, 1)
	assert.Equal(t, id1, active[0].ID)

	// Update last seen for cluster 1
	run2 := &ClusterRun{
		ID: "CR_lifecycle_2", NPoints: 10, NClusters: 1, NNoise: 2,
		MinClusterSize: 5, DurationMS: 15, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run2))
	require.NoError(t, store.UpdateClusterLastSeen(id1, "CR_lifecycle_2"))

	active, err = store.GetActiveClusterIdentities()
	require.NoError(t, err)
	assert.Equal(t, "CR_lifecycle_2", active[0].LastSeenRunID)
}

func TestEmbeddingStore_ClusterSnapshotsAndEvents(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create run and cluster
	run := &ClusterRun{
		ID: "CR_snap", NPoints: 10, NClusters: 1, NNoise: 0,
		MinClusterSize: 5, DurationMS: 10, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	id0, err := store.CreateCluster("CR_snap")
	require.NoError(t, err)

	// Save snapshots
	snapshots := []ClusterSnapshot{
		{ClusterID: id0, RunID: "CR_snap", Centroid: createTestEmbedding(384), NMembers: 8},
	}
	require.NoError(t, store.SaveClusterSnapshots(snapshots))

	// Record events
	sim := 0.95
	events := []ClusterEvent{
		{RunID: "CR_snap", EventType: "birth", ClusterID: id0},
		{RunID: "CR_snap", EventType: "stable", ClusterID: id0, Similarity: &sim},
	}
	require.NoError(t, store.RecordClusterEvents(events))

	// Verify events were saved by counting
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM cluster_events WHERE run_id = ?`, "CR_snap").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify snapshot was saved
	err = db.QueryRow(`SELECT COUNT(*) FROM cluster_snapshots WHERE run_id = ?`, "CR_snap").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestEmbeddingStore_GetLabelEligibleClusters(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create run (FK target)
	run := &ClusterRun{
		ID: "CR_label_elig", NPoints: 30, NClusters: 3, NNoise: 0,
		MinClusterSize: 3, DurationMS: 5, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	// Create 3 clusters
	c1, err := store.CreateCluster("CR_label_elig")
	require.NoError(t, err)
	c2, err := store.CreateCluster("CR_label_elig")
	require.NoError(t, err)
	c3, err := store.CreateCluster("CR_label_elig")
	require.NoError(t, err)

	// Assign embeddings: c1=5, c2=10, c3=2
	for i := 0; i < 5; i++ {
		emb := &EmbeddingModel{
			SourceType: "attestation", SourceID: generateTestID(),
			Text: fmt.Sprintf("c1 text %d", i), Embedding: createTestEmbedding(384),
			Model: "test", Dimensions: 384,
		}
		require.NoError(t, store.Save(emb))
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{{ID: emb.ID, ClusterID: c1, Probability: 0.9}}))
	}
	for i := 0; i < 10; i++ {
		emb := &EmbeddingModel{
			SourceType: "attestation", SourceID: generateTestID(),
			Text: fmt.Sprintf("c2 text %d", i), Embedding: createTestEmbedding(384),
			Model: "test", Dimensions: 384,
		}
		require.NoError(t, store.Save(emb))
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{{ID: emb.ID, ClusterID: c2, Probability: 0.9}}))
	}
	for i := 0; i < 2; i++ {
		emb := &EmbeddingModel{
			SourceType: "attestation", SourceID: generateTestID(),
			Text: fmt.Sprintf("c3 text %d", i), Embedding: createTestEmbedding(384),
			Model: "test", Dimensions: 384,
		}
		require.NoError(t, store.Save(emb))
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{{ID: emb.ID, ClusterID: c3, Probability: 0.9}}))
	}

	// minSize=5 should return c1 (5) and c2 (10), not c3 (2)
	eligible, err := store.GetLabelEligibleClusters(5, 7, 10)
	require.NoError(t, err)
	assert.Len(t, eligible, 2)
	// Ordered by member count desc: c2 (10) first, c1 (5) second
	assert.Equal(t, c2, eligible[0].ID)
	assert.Equal(t, 10, eligible[0].Members)
	assert.Equal(t, c1, eligible[1].ID)
	assert.Equal(t, 5, eligible[1].Members)

	// Label c2 — it should be excluded by cooldown
	require.NoError(t, store.UpdateClusterLabel(c2, "test label"))
	eligible, err = store.GetLabelEligibleClusters(5, 7, 10)
	require.NoError(t, err)
	assert.Len(t, eligible, 1)
	assert.Equal(t, c1, eligible[0].ID)

	// Limit=1 should only return top cluster
	eligible, err = store.GetLabelEligibleClusters(1, 7, 1)
	require.NoError(t, err)
	assert.Len(t, eligible, 1)

	// Backdate c2's labeled_at beyond cooldown — should become eligible again
	_, err = db.Exec(`UPDATE clusters SET labeled_at = datetime('now', '-10 days') WHERE id = ?`, c2)
	require.NoError(t, err)
	eligible, err = store.GetLabelEligibleClusters(5, 7, 10)
	require.NoError(t, err)
	assert.Len(t, eligible, 2, "cluster with expired cooldown should be eligible again")
	assert.Equal(t, c2, eligible[0].ID, "c2 has more members, should be first")
}

func TestEmbeddingStore_SampleClusterTexts(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create run and cluster
	run := &ClusterRun{
		ID: "CR_sample", NPoints: 10, NClusters: 1, NNoise: 0,
		MinClusterSize: 3, DurationMS: 5, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	cid, err := store.CreateCluster("CR_sample")
	require.NoError(t, err)

	// Add 5 embeddings to the cluster
	expectedTexts := map[string]bool{}
	for i := 0; i < 5; i++ {
		text := fmt.Sprintf("sample text %d", i)
		expectedTexts[text] = true
		emb := &EmbeddingModel{
			SourceType: "attestation", SourceID: generateTestID(),
			Text: text, Embedding: createTestEmbedding(384),
			Model: "test", Dimensions: 384,
		}
		require.NoError(t, store.Save(emb))
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{{ID: emb.ID, ClusterID: cid, Probability: 0.9}}))
	}

	// Sample 3 — should get 3 texts that are all from the cluster
	texts, err := store.SampleClusterTexts(cid, 3)
	require.NoError(t, err)
	assert.Len(t, texts, 3)
	for _, text := range texts {
		assert.True(t, expectedTexts[text], "unexpected text: %s", text)
	}

	// Sample more than available — should get all 5
	texts, err = store.SampleClusterTexts(cid, 100)
	require.NoError(t, err)
	assert.Len(t, texts, 5)
}

func TestEmbeddingStore_UpdateClusterLabel(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	run := &ClusterRun{
		ID: "CR_label", NPoints: 5, NClusters: 1, NNoise: 0,
		MinClusterSize: 3, DurationMS: 5, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	cid, err := store.CreateCluster("CR_label")
	require.NoError(t, err)

	// Label should initially be nil
	active, err := store.GetActiveClusterIdentities()
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Nil(t, active[0].Label)

	// Set label
	require.NoError(t, store.UpdateClusterLabel(cid, "Technology & Software"))

	// Verify label was set
	active, err = store.GetActiveClusterIdentities()
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.NotNil(t, active[0].Label)
	assert.Equal(t, "Technology & Software", *active[0].Label)

	// Verify labeled_at was set
	var labeledAt *string
	err = db.QueryRow(`SELECT labeled_at FROM clusters WHERE id = ?`, cid).Scan(&labeledAt)
	require.NoError(t, err)
	require.NotNil(t, labeledAt)
}

func TestEmbeddingStore_GetClusterDetails(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop()
	store := NewEmbeddingStore(db, logger)

	// Create run, cluster, and some embeddings assigned to it
	run := &ClusterRun{
		ID: "CR_details", NPoints: 5, NClusters: 1, NNoise: 1,
		MinClusterSize: 3, DurationMS: 5, CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.CreateClusterRun(run))

	cid, err := store.CreateCluster("CR_details")
	require.NoError(t, err)

	// Save some embeddings and assign them to the cluster
	for i := 0; i < 3; i++ {
		emb := &EmbeddingModel{
			SourceType: "attestation",
			SourceID:   generateTestID(),
			Text:       fmt.Sprintf("detail test %d", i),
			Embedding:  createTestEmbedding(384),
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		}
		require.NoError(t, store.Save(emb))
		require.NoError(t, store.UpdateClusterAssignments([]ClusterAssignment{
			{ID: emb.ID, ClusterID: cid, Probability: 0.9},
		}))
	}

	details, err := store.GetClusterDetails()
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, cid, details[0].ID)
	assert.Equal(t, 3, details[0].Members)
	assert.Equal(t, "active", details[0].Status)
}
