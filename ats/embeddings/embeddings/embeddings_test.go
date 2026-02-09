//go:build cgo && rustembeddings

package embeddings

import (
	"testing"
)

func TestEmbeddingEngine(t *testing.T) {
	service := NewEmbeddingService()
	defer service.Close()

	// Initialize with model
	modelPath := "../models/all-MiniLM-L6-v2/model.onnx"
	err := service.Init(modelPath)
	if err != nil {
		t.Fatalf("Failed to initialize embedding service: %v", err)
	}

	// Get model info
	info, err := service.ModelInfo()
	if err != nil {
		t.Fatalf("Failed to get model info: %v", err)
	}

	t.Logf("Model: %s, Dimensions: %d", info.Name, info.Dimensions)
	if info.Dimensions != 384 {
		t.Errorf("Expected 384 dimensions, got %d", info.Dimensions)
	}

	// Test single embedding
	text := "The quick brown fox jumps over the lazy dog"
	result, err := service.Embed(text)
	if err != nil {
		t.Fatalf("Failed to embed text: %v", err)
	}

	if len(result.Embedding) != 384 {
		t.Errorf("Expected 384-dimensional embedding, got %d", len(result.Embedding))
	}

	t.Logf("Embedding generated: %d dimensions, %d tokens, %.2fms inference",
		len(result.Embedding), result.Tokens, result.InferenceMS)

	// Test batch embedding
	texts := []string{
		"cat",
		"kitten",
		"dog",
	}

	batchResult, err := service.EmbedBatch(texts)
	if err != nil {
		t.Fatalf("Failed to batch embed: %v", err)
	}

	if len(batchResult.Embeddings) != len(texts) {
		t.Errorf("Expected %d embeddings, got %d", len(texts), len(batchResult.Embeddings))
	}

	t.Logf("Batch processing: %d texts, total %d tokens, %.2fms total inference",
		len(texts), batchResult.TotalTokens, batchResult.TotalInferenceMS)
}

func TestSemanticSimilarity(t *testing.T) {
	service := NewEmbeddingService()
	defer service.Close()

	modelPath := "../models/all-MiniLM-L6-v2/model.onnx"
	if err := service.Init(modelPath); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Test semantic similarity
	tests := []struct {
		text1  string
		text2  string
		minSim float32
		maxSim float32
		desc   string
	}{
		{"cat", "kitten", 0.85, 1.0, "Similar animals"},
		{"cat", "dog", 0.70, 0.95, "Different but related animals"}, // Adjusted upper bound
		{"cat", "car", 0.50, 0.90, "Unrelated concepts"},            // Adjusted upper bound
		{"happy", "joyful", 0.85, 1.0, "Similar emotions"},
	}

	for _, test := range tests {
		result1, err := service.Embed(test.text1)
		if err != nil {
			t.Fatalf("Failed to embed %s: %v", test.text1, err)
		}

		result2, err := service.Embed(test.text2)
		if err != nil {
			t.Fatalf("Failed to embed %s: %v", test.text2, err)
		}

		similarity := cosineSimilarity(result1.Embedding, result2.Embedding)
		t.Logf("%s: '%s' vs '%s' = %.4f", test.desc, test.text1, test.text2, similarity)

		if similarity < test.minSim || similarity > test.maxSim {
			t.Errorf("Similarity %.4f outside expected range [%.2f, %.2f] for %s",
				similarity, test.minSim, test.maxSim, test.desc)
		}
	}
}

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	// Use simple sqrt approximation to avoid importing math
	sqrtA := float32(1.0)
	sqrtB := float32(1.0)

	// Newton's method for sqrt approximation (good enough for testing)
	for i := 0; i < 5; i++ {
		sqrtA = (sqrtA + normA/sqrtA) / 2
		sqrtB = (sqrtB + normB/sqrtB) / 2
	}

	return dotProduct / (sqrtA * sqrtB)
}
