package embeddings

import (
	"math"
	"testing"
)

func TestPluginServiceSerializeDeserialize(t *testing.T) {
	svc := &PluginEmbeddingService{}

	original := []float32{0.1, -0.5, 0.0, 1.0, -1.0, 3.14}

	blob, err := svc.SerializeEmbedding(original)
	if err != nil {
		t.Fatalf("SerializeEmbedding failed: %v", err)
	}

	if len(blob) != len(original)*4 {
		t.Fatalf("expected blob length %d, got %d", len(original)*4, len(blob))
	}

	restored, err := svc.DeserializeEmbedding(blob)
	if err != nil {
		t.Fatalf("DeserializeEmbedding failed: %v", err)
	}

	if len(restored) != len(original) {
		t.Fatalf("expected %d floats, got %d", len(original), len(restored))
	}

	for i := range original {
		if original[i] != restored[i] {
			t.Errorf("mismatch at index %d: expected %f, got %f", i, original[i], restored[i])
		}
	}
}

func TestPluginServiceSerializeEmpty(t *testing.T) {
	svc := &PluginEmbeddingService{}

	_, err := svc.SerializeEmbedding([]float32{})
	if err == nil {
		t.Error("expected error for empty embedding")
	}
}

func TestPluginServiceDeserializeInvalidLength(t *testing.T) {
	svc := &PluginEmbeddingService{}

	_, err := svc.DeserializeEmbedding([]byte{0x01, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for invalid length")
	}
}

func TestPluginServiceComputeSimilarity(t *testing.T) {
	svc := &PluginEmbeddingService{}

	// Identical vectors → similarity = 1.0
	a := []float32{1, 0, 0}
	sim, err := svc.ComputeSimilarity(a, a)
	if err != nil {
		t.Fatalf("ComputeSimilarity failed: %v", err)
	}
	if math.Abs(float64(sim)-1.0) > 1e-6 {
		t.Errorf("identical vectors: expected similarity ~1.0, got %f", sim)
	}

	// Orthogonal vectors → similarity = 0.0
	b := []float32{0, 1, 0}
	sim, err = svc.ComputeSimilarity(a, b)
	if err != nil {
		t.Fatalf("ComputeSimilarity failed: %v", err)
	}
	if math.Abs(float64(sim)) > 1e-6 {
		t.Errorf("orthogonal vectors: expected similarity ~0.0, got %f", sim)
	}

	// Opposite vectors → similarity = -1.0
	c := []float32{-1, 0, 0}
	sim, err = svc.ComputeSimilarity(a, c)
	if err != nil {
		t.Fatalf("ComputeSimilarity failed: %v", err)
	}
	if math.Abs(float64(sim)+1.0) > 1e-6 {
		t.Errorf("opposite vectors: expected similarity ~-1.0, got %f", sim)
	}
}

func TestPluginServiceComputeSimilarityDimensionMismatch(t *testing.T) {
	svc := &PluginEmbeddingService{}

	_, err := svc.ComputeSimilarity([]float32{1, 2}, []float32{1, 2, 3})
	if err == nil {
		t.Error("expected error for dimension mismatch")
	}
}
