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

	// Should create 1 node: alice (subject only)
	// "engineer" doesn't appear as subject, so it's skipped (two-pass approach)
	if len(graph.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(graph.Nodes))
	}

	// Should create 0 links (engineer is not an entity in this attestation set)
	if len(graph.Links) != 0 {
		t.Errorf("Expected 0 links, got %d", len(graph.Links))
	}

	// Verify stats
	if graph.Meta.Stats.TotalNodes != 1 {
		t.Errorf("Meta TotalNodes = %d, want 1", graph.Meta.Stats.TotalNodes)
	}

	if graph.Meta.Stats.TotalEdges != 0 {
		t.Errorf("Meta TotalEdges = %d, want 0", graph.Meta.Stats.TotalEdges)
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

	// Should create 1 node: alice (subject only)
	// Rust and Java don't appear as subjects, so they're skipped (two-pass approach)
	if len(graph.Nodes) != 1 {
		t.Errorf("Expected 1 node (alice), got %d", len(graph.Nodes))
	}

	// Should create 0 links (Rust/Java are not entities in this attestation set)
	if len(graph.Links) != 0 {
		t.Errorf("Expected 0 links, got %d", len(graph.Links))
	}

	// Verify alice node exists
	if graph.Nodes[0].ID != "alice" {
		t.Errorf("Expected alice node, got %s", graph.Nodes[0].ID)
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

// TestGroupingContextsSkipped validates that grouping contexts (commit_metadata, lineage, authorship)
// don't create nodes since they never appear as subjects. Fixes issue where these were appearing in graph.
func TestGroupingContextsSkipped(t *testing.T) {
	builder := createTestBuilder(t)

	// Simulate git ingestion: commits with grouping contexts
	attestations := []types.As{
		{
			Subjects:   []string{"abc123"},
			Predicates: []string{"is_commit"},
			Contexts:   []string{"commit_metadata"},
			Timestamp:  time.Now(),
		},
		{
			Subjects:   []string{"abc123"},
			Predicates: []string{"in_lineage"},
			Contexts:   []string{"lineage"},
			Timestamp:  time.Now(),
		},
		{
			Subjects:   []string{"abc123"},
			Predicates: []string{"has_authorship"},
			Contexts:   []string{"authorship"},
			Timestamp:  time.Now(),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "test")

	// Only abc123 should become a node (appears as subject)
	// commit_metadata, lineage, authorship should NOT (never appear as subjects)
	if len(graph.Nodes) != 1 {
		t.Errorf("Expected 1 node (abc123), got %d", len(graph.Nodes))
		for _, node := range graph.Nodes {
			t.Logf("  Found node: %s", node.ID)
		}
	}

	// No links should be created (grouping contexts are skipped)
	if len(graph.Links) != 0 {
		t.Errorf("Expected 0 links, got %d", len(graph.Links))
	}

	// Verify the commit node exists
	if graph.Nodes[0].ID != "abc123" {
		t.Errorf("Expected node abc123, got %s", graph.Nodes[0].ID)
	}
}
