package graph

import (
	"context"
	"strings"
	"testing"
)

// TestBuildFromQueryEmpty tests empty query handling
func TestBuildFromQueryEmpty(t *testing.T) {
	builder := createTestBuilder(t)

	tests := []string{
		"",
		"   ",
		"\n",
		"\t",
		"  \n  \t  ",
	}

	for _, query := range tests {
		graph, err := builder.BuildFromQuery(context.Background(), query, 100)
		if err != nil {
			t.Errorf("BuildFromQuery(%q) returned error: %v", query, err)
		}

		if len(graph.Nodes) != 0 {
			t.Errorf("Empty query should produce 0 nodes, got %d", len(graph.Nodes))
		}

		if len(graph.Links) != 0 {
			t.Errorf("Empty query should produce 0 links, got %d", len(graph.Links))
		}
	}
}

// TestBuildFromQueryWithQuotes tests quote handling in queries
func TestBuildFromQueryWithQuotes(t *testing.T) {
	builder := createTestBuilder(t)

	tests := []struct {
		name        string
		query       string
		shouldParse bool
		description string
	}{
		{
			name:        "temporal_with_double_quotes",
			query:       `ax since "2024-11-01"`,
			shouldParse: true,
			description: "Temporal query with quoted date",
		},
		{
			name:        "temporal_with_single_quotes",
			query:       `ax since '2024-11-01'`,
			shouldParse: true,
			description: "Temporal query with single-quoted date",
		},
		{
			name:        "multi_word_context_double_quotes",
			query:       `ax is engineer of "Tech Corp"`,
			shouldParse: true,
			description: "Multi-word context with double quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := builder.BuildFromQuery(context.Background(), tt.query, 100)

			if tt.shouldParse {
				if err != nil && !strings.Contains(err.Error(), "warning") {
					t.Errorf("Query %q (%s) should parse but got error: %v",
						tt.query, tt.description, err)
				}
				if graph == nil {
					t.Errorf("Query %q (%s) should return graph (even if empty), got nil",
						tt.query, tt.description)
				}
			}
		})
	}
}
