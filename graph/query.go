package graph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
	grapherr "github.com/teranos/QNTX/graph/error"
	"github.com/teranos/QNTX/logger"
)

// BuildFromRecentAttestations builds a graph from the most recent attestations in the database
func (b *AxGraphBuilder) BuildFromRecentAttestations(ctx context.Context, limit int) (*Graph, error) {
	// Query all attestations using the executor (empty filter returns all)
	result, err := b.executor.ExecuteAsk(ctx, types.AxFilter{
		Limit: limit,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to query attestations: %w", err)
	}

	b.logger.Infof("Building graph from %d recent attestations", len(result.Attestations))

	// Build graph from attestations
	graph := b.buildGraphFromAttestations(result.Attestations, "recent attestations")
	return graph, nil
}

// BuildFromQuery executes an Ax query and builds a graph from the results
func (b *AxGraphBuilder) BuildFromQuery(ctx context.Context, query string, limit int) (*Graph, error) {
	// Handle empty or whitespace-only queries
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		b.logger.Debugw("Empty query received")
		return &Graph{
			Nodes: []Node{},
			Links: []Link{},
			Meta: Meta{
				GeneratedAt: time.Now(),
				Stats: Stats{
					TotalNodes: 0,
					TotalEdges: 0,
				},
				Config: map[string]string{
					"query":       "",
					"description": "Type an Ax query to see the graph...",
				},
			},
		}, nil
	}

	b.logger.Debugw("Building graph from query", "query_length", len(trimmedQuery))

	// Parse the query string into an AskFilter
	// Split by newlines for multi-line queries
	queryLines := strings.Split(trimmedQuery, "\n")
	var allArgs []string
	for _, line := range queryLines {
		line = strings.TrimSpace(line)
		if line != "" {
			// Parse args respecting quotes (like shell does)
			args, err := shellquote.Split(line)
			if err != nil {
				// If quote parsing fails, fall back to simple split
				b.logger.Debugw("Quote parsing failed, using simple split", "line", line, "error", err)
				args = strings.Fields(line)
			}
			allArgs = append(allArgs, args...)
		}
	}

	if len(allArgs) == 0 {
		return b.BuildFromQuery(ctx, "", limit) // Return empty graph
	}

	b.logger.Debugw("Parsed query args", "arg_count", len(allArgs))
	if logger.ShouldLogAll(b.verbosity) {
		b.logger.Debugw("Query arguments", "args", allArgs)
	}

	// Parse query
	filter, err := parser.ParseAxCommand(allArgs)
	if err != nil {
		graphErr := grapherr.New(
			grapherr.CategoryParse,
			err,
			"Invalid query syntax - check your operators and values",
		).WithSubcategory(grapherr.SubcategoryParseInvalidSyntax).
			WithContext("query", trimmedQuery).
			WithContext("args", allArgs)

		b.logger.Warnw("Query parse failed", graphErr.ToLogFields()...)

		// Return empty graph with error in meta
		return &Graph{
			Nodes: []Node{},
			Links: []Link{},
			Meta: Meta{
				GeneratedAt: time.Now(),
				Stats: Stats{
					TotalNodes: 0,
					TotalEdges: 0,
				},
				Config: graphErr.ToGraphMeta(),
			},
		}, graphErr
	}

	b.logger.Debugw("Query filter created")
	if logger.ShouldLogTrace(b.verbosity) {
		b.logger.Debugw("Filter details", "filter", fmt.Sprintf("%+v", filter))
	}

	// Apply limit to filter (centralized graph limit control)
	if limit > 0 {
		filter.Limit = limit
	}

	// Execute the Ax query
	result, err := b.executor.ExecuteAsk(ctx, *filter)
	if err != nil {
		graphErr := grapherr.New(
			grapherr.CategoryQuery,
			err,
			"Query execution failed - database error",
		).WithSubcategory(grapherr.SubcategoryQueryExecution).
			WithContext("query", trimmedQuery)

		b.logger.Errorw("Query execution failed", graphErr.ToLogFields()...)

		// Return empty graph with error in meta
		return &Graph{
			Nodes: []Node{},
			Links: []Link{},
			Meta: Meta{
				GeneratedAt: time.Now(),
				Stats: Stats{
					TotalNodes: 0,
					TotalEdges: 0,
				},
				Config: graphErr.ToGraphMeta(),
			},
		}, graphErr
	}

	b.logger.Infow("Query executed successfully",
		"attestations", len(result.Attestations),
	)

	if logger.ShouldLogAll(b.verbosity) {
		b.logger.Debugw("Attestation results", "results", fmt.Sprintf("%+v", result))
	}

	// Build graph from attestation results
	graph := b.buildGraphFromAttestations(result.Attestations, trimmedQuery)

	b.logger.Infow("Graph built",
		"nodes", len(graph.Nodes),
		"links", len(graph.Links),
	)

	return graph, nil
}
