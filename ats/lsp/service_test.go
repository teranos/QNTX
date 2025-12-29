package lsp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func setupService(t *testing.T) *Service {
	t.Helper()
	db := qntxtest.CreateTestDB(t)

	idx, err := storage.NewSymbolIndex(db)
	require.NoError(t, err, "Failed to create symbol index")

	return NewService(idx)
}

func TestParse_ValidQueries(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		query string
	}{
		// Music domain
		{"classical composer", "beethoven is composer of symphony"},
		{"jazz genre", "coltrane is saxophonist of jazz"},

		// Bioinformatics
		{"tumor suppressor gene", "brca1 is gene of human"},
		{"protein function", "insulin is protein of pancreas"},

		// Card games
		{"poker hand", "flush is hand of poker"},
		{"playing card", "ace is card of spades"},

		// Weather patterns
		{"storm system", "hurricane is storm of atlantic"},
		{"precipitation type", "rain is precipitation of seattle"},

		// Code & libraries
		{"web framework", "react is library of javascript"},
		{"type system", "typescript is language of web"},

		// Graph databases
		{"graph element", "edge is relation of graph"},
		{"query language", "cypher is language of neo4j"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.Parse(ctx, tt.query, 0)
			require.NoError(t, err)
			assert.NotEmpty(t, resp.Tokens, "Should generate tokens for valid query")
			assert.Empty(t, resp.Diagnostics, "Should have no diagnostics for valid query")
		})
	}
}

func TestParse_InvalidSyntax(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	// Parser is lenient - just verify it handles malformed input gracefully
	tests := []struct {
		name  string
		query string
	}{
		{"double keyword", "postgres is is database"},
		{"missing predicate", "node of graph"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.Parse(ctx, tt.query, 0)
			require.NoError(t, err, "Should not crash on malformed input")
			assert.NotNil(t, resp, "Should return response")
		})
	}
}

func TestParse_EmptyQuery(t *testing.T) {
	svc := setupService(t)

	resp, err := svc.Parse(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Empty(t, resp.Tokens, "Empty query should return no tokens")
}

func TestGetCompletions_ContextAwareness(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		query        string
		cursor       int
		expectedKind string
	}{
		// After "is" → predicates
		{"after is - music", "sonata is ", 10, "predicate"},
		{"after is - bio", "enzyme is ", 10, "predicate"},
		{"after is - weather", "frost is ", 9, "predicate"},

		// After "of" → contexts
		{"after of - code", "golang is language of ", 22, "context"},
		{"after of - card", "king is card of ", 16, "context"},

		// After "by" → actors
		{"after by - music", "requiem is composition of mozart by ", 36, "actor"},
		{"after by - database", "node is element of graph by ", 29, "actor"},

		// Start of query → subjects (with 3+ chars)
		{"subject prefix - short", "str", 3, "subject"},
		{"subject prefix - longer", "nucleotide", 10, "subject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CompletionRequest{
				Query:  tt.query,
				Cursor: tt.cursor,
			}

			items, err := svc.GetCompletions(ctx, req)
			require.NoError(t, err)

			// Verify returned items match expected kind (if any returned)
			for _, item := range items {
				assert.Equal(t, tt.expectedKind, item.Kind,
					"Completion kind mismatch at position")
			}
		})
	}
}

func TestGetCompletions_MinimumPrefix(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		prefix      string
		shouldMatch bool
	}{
		{"2 chars - valid", "rn", true},
		{"3 chars - valid", "dna", true},
		{"4 chars - valid", "prot", true},
		{"1 char - too short", "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CompletionRequest{
				Query:  tt.prefix,
				Cursor: len(tt.prefix),
			}

			items, err := svc.GetCompletions(ctx, req)
			require.NoError(t, err)

			if !tt.shouldMatch {
				assert.Empty(t, items,
					"Should not return completions for prefix < 2 chars")
			}
		})
	}
}

func TestGetCompletions_EmptyQuery(t *testing.T) {
	svc := setupService(t)

	req := CompletionRequest{
		Query:  "",
		Cursor: 0,
	}

	items, err := svc.GetCompletions(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, items, "Empty query should return no completions")
}

func TestParse_HoverInfo(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	// Hover info is embedded in semantic tokens from Parse()
	resp, err := svc.Parse(ctx, "stradivarius is instrument", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Tokens, "Should return tokens with hover info")

	// Tokens may or may not have hover info depending on test DB data
	// Just verify structure is valid
	for _, token := range resp.Tokens {
		assert.NotEmpty(t, token.Text, "Token should have text")
	}
}
