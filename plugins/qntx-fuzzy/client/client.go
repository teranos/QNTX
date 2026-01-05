// Package client provides a Go client for the qntx-fuzzy Rust service.
//
// This client implements the same interface as the built-in FuzzyMatcher,
// allowing it to be used as a drop-in replacement in AxExecutor.
package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/teranos/QNTX/plugins/qntx-fuzzy/client/proto"
)

// VocabularyType specifies which vocabulary to search
type VocabularyType int32

const (
	VocabularyPredicates VocabularyType = 0
	VocabularyContexts   VocabularyType = 1
)

// RankedMatch represents a fuzzy match result with score
type RankedMatch struct {
	Value    string
	Score    float64
	Strategy string
}

// Client wraps the gRPC connection to the fuzzy matching service
type Client struct {
	conn   *grpc.ClientConn
	client pb.FuzzyMatchServiceClient

	mu          sync.RWMutex
	indexHash   string
	lastRefresh time.Time
}

// Config holds client configuration
type Config struct {
	// Address of the fuzzy service (e.g., "localhost:9100")
	Address string

	// RefreshInterval controls how often to check for vocabulary changes
	RefreshInterval time.Duration

	// ConnectTimeout for initial connection
	ConnectTimeout time.Duration

	// RequestTimeout for individual requests
	RequestTimeout time.Duration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Address:         "localhost:9100",
		RefreshInterval: 30 * time.Second,
		ConnectTimeout:  5 * time.Second,
		RequestTimeout:  100 * time.Millisecond,
	}
}

// NewClient creates a new fuzzy matching client
func NewClient(cfg Config) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to fuzzy service at %s: %w", cfg.Address, err)
	}

	return &Client{
		conn:   conn,
		client: pb.NewFuzzyMatchServiceClient(conn),
	}, nil
}

// Close closes the gRPC connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// RebuildIndex sends new vocabulary to the service
func (c *Client) RebuildIndex(ctx context.Context, predicates, contexts []string) error {
	resp, err := c.client.RebuildIndex(ctx, &pb.RebuildIndexRequest{
		Predicates: predicates,
		Contexts:   contexts,
	})
	if err != nil {
		return fmt.Errorf("failed to rebuild index: %w", err)
	}

	c.mu.Lock()
	c.indexHash = resp.IndexHash
	c.lastRefresh = time.Now()
	c.mu.Unlock()

	return nil
}

// FindMatches finds vocabulary items matching a query
func (c *Client) FindMatches(ctx context.Context, query string, vocabType VocabularyType, limit int, minScore float64) ([]RankedMatch, error) {
	resp, err := c.client.FindMatches(ctx, &pb.FindMatchesRequest{
		Query:           query,
		VocabularyType:  pb.VocabularyType(vocabType),
		Limit:           int32(limit),
		MinScore:        minScore,
		IncludeStrategy: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find matches: %w", err)
	}

	matches := make([]RankedMatch, len(resp.Matches))
	for i, m := range resp.Matches {
		matches[i] = RankedMatch{
			Value:    m.Value,
			Score:    m.Score,
			Strategy: m.Strategy,
		}
	}

	return matches, nil
}

// FindPredicateMatches is a convenience method for predicate matching
func (c *Client) FindPredicateMatches(ctx context.Context, query string) ([]string, error) {
	matches, err := c.FindMatches(ctx, query, VocabularyPredicates, 20, 0.6)
	if err != nil {
		return nil, err
	}

	values := make([]string, len(matches))
	for i, m := range matches {
		values[i] = m.Value
	}
	return values, nil
}

// FindContextMatches is a convenience method for context matching
func (c *Client) FindContextMatches(ctx context.Context, query string) ([]string, error) {
	matches, err := c.FindMatches(ctx, query, VocabularyContexts, 20, 0.6)
	if err != nil {
		return nil, err
	}

	values := make([]string, len(matches))
	for i, m := range matches {
		values[i] = m.Value
	}
	return values, nil
}

// GetStats returns service statistics
func (c *Client) GetStats(ctx context.Context) (*Stats, error) {
	resp, err := c.client.GetStats(ctx, &pb.StatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return &Stats{
		PredicateCount:  resp.PredicateCount,
		ContextCount:    resp.ContextCount,
		IndexHash:       resp.IndexHash,
		QueriesServed:   resp.QueriesServed,
		AvgQueryTimeUs:  resp.AvgQueryTimeUs,
		UptimeSeconds:   resp.UptimeSeconds,
	}, nil
}

// Health checks if the service is healthy
func (c *Client) Health(ctx context.Context) (bool, bool, error) {
	resp, err := c.client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return false, false, fmt.Errorf("health check failed: %w", err)
	}
	return resp.Healthy, resp.IndexReady, nil
}

// Stats contains service statistics
type Stats struct {
	PredicateCount int64
	ContextCount   int64
	IndexHash      string
	QueriesServed  int64
	AvgQueryTimeUs int64
	UptimeSeconds  int64
}
