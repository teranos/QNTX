package graph

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// TestExplicitTypeAttestations tests that explicit type attestations are extracted and applied
func TestExplicitTypeAttestations(t *testing.T) {
	builder := createTestBuilder(t)

	// Music domain: "as dark_side_of_the_moon node_type album"
	attestations := []types.As{
		{
			Subjects:   []string{"dark_side_of_the_moon"},
			Predicates: []string{"node_type"},
			Contexts:   []string{"album"},
			Actors:     []string{"ix-music"},
			Timestamp:  time.Now(),
		},
	}

	graph := builder.buildGraphFromAttestations(attestations, "node type test")

	if len(graph.Nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(graph.Nodes))
	}

	node := graph.Nodes[0]

	if node.Type != "album" {
		t.Errorf("Node type = %q, want %q", node.Type, "album")
	}

	if node.TypeSource != "attested" {
		t.Errorf("Node TypeSource = %q, want %q", node.TypeSource, "attested")
	}
}

// TestExtractTypeDefinitions tests type definition extraction with display metadata
func TestExtractTypeDefinitions(t *testing.T) {
	builder := createTestBuilder(t)

	// Music domain type definitions with visual styling
	attestations := []types.As{
		{
			Subjects:   []string{"artist"},
			Predicates: []string{"type"},
			Contexts:   []string{"graph"},
			Actors:     []string{"artist"},
			Timestamp:  time.Now(),
			Attributes: map[string]interface{}{
				"display_color": "#e74c3c",
				"display_label": "Artist",
				"opacity":       0.9,
			},
		},
		{
			Subjects:   []string{"genre"},
			Predicates: []string{"type"},
			Contexts:   []string{"graph"},
			Actors:     []string{"genre"},
			Timestamp:  time.Now(),
			Attributes: map[string]interface{}{
				"display_color": "#3498db",
				"display_label": "Genre",
				"deprecated":    false,
			},
		},
	}

	typeDefs := builder.extractTypeDefinitions(attestations)

	if len(typeDefs) != 2 {
		t.Fatalf("Expected 2 type definitions, got %d", len(typeDefs))
	}

	// Verify artist type definition
	artistDef, exists := typeDefs["artist"]
	if !exists {
		t.Fatal("artist type definition not found")
	}

	if artistDef.Color != "#e74c3c" {
		t.Errorf("artistDef.Color = %q, want %q", artistDef.Color, "#e74c3c")
	}

	if artistDef.Label != "Artist" {
		t.Errorf("artistDef.Label = %q, want %q", artistDef.Label, "Artist")
	}

	if artistDef.Opacity == nil || *artistDef.Opacity != 0.9 {
		var opacity float64 = -1
		if artistDef.Opacity != nil {
			opacity = *artistDef.Opacity
		}
		t.Errorf("artistDef.Opacity = %f, want 0.9", opacity)
	}

	// Verify genre type definition (with default opacity)
	genreDef, exists := typeDefs["genre"]
	if !exists {
		t.Fatal("genre type definition not found")
	}

	// Default opacity should be 1.0 when not specified
	if genreDef.Opacity == nil || *genreDef.Opacity != 1.0 {
		var opacity float64 = -1
		if genreDef.Opacity != nil {
			opacity = *genreDef.Opacity
		}
		t.Errorf("genreDef.Opacity = %f, want 1.0 (default)", opacity)
	}

	if genreDef.Deprecated != false {
		t.Errorf("genreDef.Deprecated = %v, want false", genreDef.Deprecated)
	}
}

// TestExtractTypeDefinitionsEmpty tests empty type definitions
func TestExtractTypeDefinitionsEmpty(t *testing.T) {
	builder := createTestBuilder(t)

	// Regular attestations without type definitions
	attestations := []types.As{
		{
			Subjects:   []string{"pink_floyd"},
			Predicates: []string{"performs"},
			Contexts:   []string{"time"},
			Timestamp:  time.Now(),
		},
	}

	typeDefs := builder.extractTypeDefinitions(attestations)

	if len(typeDefs) != 0 {
		t.Errorf("Expected 0 type definitions, got %d", len(typeDefs))
	}
}

// TestBuildFromRecentAttestations tests building graph from recent attestations
func TestBuildFromRecentAttestations(t *testing.T) {
	builder := createTestBuilder(t)

	// Note: This is an integration test that would require inserting attestations
	// into the database first. For now, test with empty database.
	graph, err := builder.BuildFromRecentAttestations(context.Background(), 10)
	if err != nil {
		t.Fatalf("BuildFromRecentAttestations failed: %v", err)
	}

	// Empty database should return empty graph
	if len(graph.Nodes) != 0 {
		t.Errorf("Expected 0 nodes from empty database, got %d", len(graph.Nodes))
	}

	if graph.Meta.Stats.TotalNodes != 0 {
		t.Errorf("Meta.Stats.TotalNodes = %d, want 0", graph.Meta.Stats.TotalNodes)
	}
}
