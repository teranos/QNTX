//go:build cgo && rustembeddings

package embeddings

import (
	"math"
	"testing"
)

var haiku = []string{
	// Quantum
	"Superposition blooms — answers multiply.",
	"Both yes and no at once, until The answer appears.",
	// Biology
	"Electrons tunnel Through protein's hidden pathways — Life bends the rules too.",
	"DNA spirals, Algorithms decode life — Patterns in the code.",
	"Life seeks distant worlds, Carbon dreams in cosmic dust — We are not alone.",
	// Wisdom
	"Words ring like clear bells, cutting through the darkest fog — light finds its own way.",
	"Honest roots run deep, though the branches may be bent, the tree still stands tall.",
	// Nature
	"Green leaves unfurl slow, Reaching toward the warm sunlight — Life finds a new way.",
	"Green web intertwined, Each thread holds the whole world up — Break one, all falls down.",
	"Soil, stream, and sky sing In balance, life sustains life — Listen, or lose all.",
}

func initService(t *testing.T) EmbeddingService {
	t.Helper()
	service := NewEmbeddingService()
	if err := service.Init("../models/all-MiniLM-L6-v2/model.onnx"); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	return service
}

func TestEmbeddingEngine(t *testing.T) {
	service := initService(t)
	defer service.Close()

	info, err := service.ModelInfo()
	if err != nil {
		t.Fatalf("Failed to get model info: %v", err)
	}

	if info.Dimensions != 384 {
		t.Errorf("Expected 384 dimensions, got %d", info.Dimensions)
	}

	result, err := service.Embed(haiku[0])
	if err != nil {
		t.Fatalf("Failed to embed text: %v", err)
	}

	if len(result.Embedding) != 384 {
		t.Errorf("Expected 384-dimensional embedding, got %d", len(result.Embedding))
	}

	batchResult, err := service.EmbedBatch(haiku)
	if err != nil {
		t.Fatalf("Failed to batch embed: %v", err)
	}

	if len(batchResult.Embeddings) != len(haiku) {
		t.Errorf("Expected %d embeddings, got %d", len(haiku), len(batchResult.Embeddings))
	}
}

func TestEmbeddingsAreUnitNormalized(t *testing.T) {
	service := initService(t)
	defer service.Close()

	for _, text := range haiku {
		result, err := service.Embed(text)
		if err != nil {
			t.Fatalf("Failed to embed %q: %v", text, err)
		}

		norm := l2Norm(result.Embedding)
		if math.Abs(float64(norm-1.0)) > 1e-5 {
			t.Errorf("Embedding for %q has L2 norm %.6f, expected 1.0", text, norm)
		}
	}
}

func TestSemanticSimilarity(t *testing.T) {
	service := initService(t)
	defer service.Close()

	tests := []struct {
		text1  string
		text2  string
		minSim float32
		maxSim float32
		desc   string
	}{
		// Quantum cluster
		{haiku[0], haiku[1], 0.30, 1.0, "Both about quantum indeterminacy"},
		// Biology cluster
		{haiku[2], haiku[3], 0.30, 1.0, "Both about biology at molecular level"},
		{haiku[3], haiku[4], 0.20, 1.0, "Both about life and science"},
		// Wisdom cluster
		{haiku[5], haiku[6], 0.15, 1.0, "Both philosophical on perseverance"},
		// Nature cluster
		{haiku[7], haiku[8], 0.30, 1.0, "Both about green growth and interconnection"},
		{haiku[8], haiku[9], 0.30, 1.0, "Both about ecological interdependence"},
	}

	for _, test := range tests {
		result1, err := service.Embed(test.text1)
		if err != nil {
			t.Fatalf("Failed to embed: %v", err)
		}

		result2, err := service.Embed(test.text2)
		if err != nil {
			t.Fatalf("Failed to embed: %v", err)
		}

		similarity := cosineSimilarity(result1.Embedding, result2.Embedding)
		t.Logf("%s: %.4f", test.desc, similarity)

		if similarity < test.minSim || similarity > test.maxSim {
			t.Errorf("Similarity %.4f outside expected range [%.2f, %.2f] for %s",
				similarity, test.minSim, test.maxSim, test.desc)
		}
	}

	// Cross-cluster: within-cluster pairs should score higher than cross-cluster pairs
	embed := func(text string) []float32 {
		t.Helper()
		r, err := service.Embed(text)
		if err != nil {
			t.Fatalf("Failed to embed: %v", err)
		}
		return r.Embedding
	}

	withinNature := cosineSimilarity(embed(haiku[8]), embed(haiku[9]))
	crossQuantumNature := cosineSimilarity(embed(haiku[0]), embed(haiku[9]))
	t.Logf("Within nature cluster: %.4f, Quantum↔Nature cross: %.4f", withinNature, crossQuantumNature)

	if withinNature <= crossQuantumNature {
		t.Errorf("Within-cluster (%.4f) should exceed cross-cluster (%.4f)", withinNature, crossQuantumNature)
	}
}

func TestDoubleInitReturnsError(t *testing.T) {
	service := initService(t)
	defer service.Close()

	err := service.Init("../models/all-MiniLM-L6-v2/model.onnx")
	if err == nil {
		t.Error("Expected error on double init, got nil")
	}
}

func TestEmbedBeforeInitReturnsError(t *testing.T) {
	service := NewEmbeddingService()
	defer service.Close()

	_, err := service.Embed("hello")
	if err == nil {
		t.Error("Expected error embedding before init, got nil")
	}
}

func l2Norm(v []float32) float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	return float32(math.Sqrt(sum))
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
	}

	normA := float64(l2Norm(a))
	normB := float64(l2Norm(b))

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dotProduct / (normA * normB))
}
