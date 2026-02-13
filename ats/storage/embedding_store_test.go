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
	results, err := store.SemanticSearch(queryEmbedding, 10, 0.0)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Results should be ordered by distance (ascending)
	for i := 1; i < len(results); i++ {
		assert.LessOrEqual(t, results[i-1].Distance, results[i].Distance)
	}

	// Test with threshold
	results, err = store.SemanticSearch(queryEmbedding, 10, 0.9)
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
	results, err := store.SemanticSearch(queryEmbedding, 10, 0.0)
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
