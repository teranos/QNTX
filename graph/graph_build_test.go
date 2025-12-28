package graph

import (
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/logger"
)

// Helper to create a test builder
func createTestBuilder(t *testing.T) *AxGraphBuilder {
	t.Helper()
	db := qntxtest.CreateTestDB(t)
	testLogger := logger.Logger.Named("test")
	builder, err := NewAxGraphBuilder(db, 0, testLogger)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	return builder
}

// TestBuildGraphFromAttestationsEmpty tests empty attestation list
func TestBuildGraphFromAttestationsEmpty(t *testing.T) {
	builder := createTestBuilder(t)
	graph := builder.buildGraphFromAttestations([]types.As{}, "test query")

	if len(graph.Nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(graph.Nodes))
	}

	if len(graph.Links) != 0 {
		t.Errorf("Expected 0 links, got %d", len(graph.Links))
	}

	if graph.Meta.Stats.TotalNodes != 0 {
		t.Errorf("Meta TotalNodes = %d, want 0", graph.Meta.Stats.TotalNodes)
	}

	if graph.Meta.Stats.TotalEdges != 0 {
		t.Errorf("Meta TotalEdges = %d, want 0", graph.Meta.Stats.TotalEdges)
	}
}

// TestBuildGraphFromAttestationsSingle tests single attestation
func TestBuildGraphFromAttestationsSingle(t *testing.T) {
	builder := createTestBuilder(t)

	attestations := []types.As{
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"is"},
			Contexts:   []string{"engineer"},
			Actors:     []string{"system"},
			Timestamp:  time.Now(),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "is engineer")

	// Should create 2 nodes: alice (subject) and engineer (object)
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Should create 1 link: alice -> engineer
	if len(graph.Links) != 1 {
		t.Errorf("Expected 1 link, got %d", len(graph.Links))
	}

	// Verify stats
	if graph.Meta.Stats.TotalNodes != 2 {
		t.Errorf("Meta TotalNodes = %d, want 2", graph.Meta.Stats.TotalNodes)
	}

	if graph.Meta.Stats.TotalEdges != 1 {
		t.Errorf("Meta TotalEdges = %d, want 1", graph.Meta.Stats.TotalEdges)
	}
}

// TestBuildGraphFromAttestationsNodeDeduplication tests that duplicate nodes are merged
func TestBuildGraphFromAttestationsNodeDeduplication(t *testing.T) {
	builder := createTestBuilder(t)

	// Two attestations with overlapping nodes
	attestations := []types.As{
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"has_skill"},
			Contexts:   []string{"Rust"},
			Timestamp:  time.Now(),
		},
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"has_skill"},
			Contexts:   []string{"Java"},
			Timestamp:  time.Now(),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "skills")

	// Should create 3 nodes: alice, Rust, Java (alice is deduplicated)
	if len(graph.Nodes) != 3 {
		t.Errorf("Expected 3 nodes (alice, Rust, Java), got %d", len(graph.Nodes))
	}

	// Should create 2 links: alice -> Rust, alice -> Java
	if len(graph.Links) != 2 {
		t.Errorf("Expected 2 links, got %d", len(graph.Links))
	}

	// Verify alice node exists only once
	aliceCount := 0
	for _, node := range graph.Nodes {
		if node.ID == "alice" {
			aliceCount++
		}
	}

	if aliceCount != 1 {
		t.Errorf("alice should appear exactly once, appeared %d times", aliceCount)
	}
}

// TestBuildGraphFromAttestationsLinkWeightAccumulation tests duplicate link weight increase
func TestBuildGraphFromAttestationsLinkWeightAccumulation(t *testing.T) {
	builder := createTestBuilder(t)

	// Same relationship appears multiple times
	attestations := []types.As{
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"worked_at"},
			Contexts:   []string{"Acme Corp"},
			Timestamp:  time.Now(),
		},
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"worked_at"},
			Contexts:   []string{"Acme Corp"},
			Timestamp:  time.Now().Add(time.Hour),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "work history")

	// Should create 2 nodes, 1 link
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	if len(graph.Links) != 1 {
		t.Errorf("Expected 1 link, got %d", len(graph.Links))
	}

	// Link weight should be > 1.0
	link := graph.Links[0]
	if link.Weight <= 1.0 {
		t.Errorf("Link weight should be > 1.0 for duplicate relationships, got %f", link.Weight)
	}
}

// TestBuildGraphFromAttestationsLiteralValues tests literal values as metadata
func TestBuildGraphFromAttestationsLiteralValues(t *testing.T) {
	builder := createTestBuilder(t)

	// Use separate attestations to avoid cartesian product confusion
	attestations := []types.As{
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"age"},
			Contexts:   []string{"30"},
			Timestamp:  time.Now(),
		},
		{
			Subjects:   []string{"alice"},
			Predicates: []string{"score"},
			Contexts:   []string{"95.5"},
			Timestamp:  time.Now(),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "attributes")

	// Should create only 1 node (alice), literals don't become nodes
	if len(graph.Nodes) != 1 {
		t.Errorf("Expected 1 node (alice), got %d", len(graph.Nodes))
	}

	// Should create 0 links (literals don't create links)
	if len(graph.Links) != 0 {
		t.Errorf("Expected 0 links for literal values, got %d", len(graph.Links))
	}

	// Verify literals are in alice's metadata
	alice := graph.Nodes[0]
	if alice.Metadata == nil {
		t.Fatal("alice should have metadata")
	}

	if alice.Metadata["age"] != "30" {
		t.Errorf("alice.Metadata[age] = %v, want 30", alice.Metadata["age"])
	}

	if alice.Metadata["score"] != "95.5" {
		t.Errorf("alice.Metadata[score] = %v, want 95.5", alice.Metadata["score"])
	}
}
