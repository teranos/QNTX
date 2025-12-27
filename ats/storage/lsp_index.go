package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

const (
	// MaxSymbolQueryResults limits the number of symbols returned in autocomplete queries
	// to prevent memory exhaustion and ensure fast UI response times
	MaxSymbolQueryResults = 1000
)

// SymbolIndex caches entities for fast autocomplete
// Maintains counts of attestations for each symbol (subject/predicate/context/actor)
// TODO(QNTX #47): Add methods for interactive hover exploration
// Needed: GetTopSubjectsForPredicate, GetTopContextsForPredicate, GetTopPredicatesForContext
// These enable showing related attestations in hover tooltips
type SymbolIndex struct {
	db         *sql.DB
	subjects   map[string]int // entity → attestation count
	predicates map[string]int
	contexts   map[string]int
	actors     map[string]int
	mu         sync.RWMutex
	lastUpdate time.Time
}

// NewSymbolIndex creates and populates a symbol index.
// Validates that the database schema supports LSP requirements:
// - attestations table exists
// - subjects, predicates, contexts, actors columns are JSON arrays
func NewSymbolIndex(db *sql.DB) (*SymbolIndex, error) {
	// Validate schema: check if attestations table exists with required columns
	var tableName string
	err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name='attestations'
	`).Scan(&tableName)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("attestations table not found - database may not be initialized")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to validate schema: %w", err)
	}

	idx := &SymbolIndex{
		db:         db,
		subjects:   make(map[string]int),
		predicates: make(map[string]int),
		contexts:   make(map[string]int),
		actors:     make(map[string]int),
	}

	// Attempt to refresh - will fail gracefully if schema is incompatible
	if err := idx.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to build symbol index (check that subjects/predicates/contexts/actors are JSON arrays): %w", err)
	}

	return idx, nil
}

// Refresh reloads the index from database
// TODO(QNTX #46): Add automatic refresh strategy (periodic or event-driven)
func (idx *SymbolIndex) Refresh() error {
	ctx := context.Background()

	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Query subjects
	subjects, err := idx.querySymbols(ctx, "subjects")
	if err != nil {
		return fmt.Errorf("failed to query subjects: %w", err)
	}
	idx.subjects = subjects

	// Query predicates
	predicates, err := idx.querySymbols(ctx, "predicates")
	if err != nil {
		return fmt.Errorf("failed to query predicates: %w", err)
	}
	idx.predicates = predicates

	// Query contexts
	contexts, err := idx.querySymbols(ctx, "contexts")
	if err != nil {
		return fmt.Errorf("failed to query contexts: %w", err)
	}
	idx.contexts = contexts

	// Query actors
	actors, err := idx.queryActors(ctx)
	if err != nil {
		return fmt.Errorf("failed to query actors: %w", err)
	}
	idx.actors = actors

	idx.lastUpdate = time.Now()
	return nil
}

// queryJSONArrayColumn extracts values and counts from a JSON array column in attestations
// filterEmpty controls whether to exclude NULL and empty string values
func (idx *SymbolIndex) queryJSONArrayColumn(ctx context.Context, column string, filterEmpty bool) (map[string]int, error) {
	whereClause := ""
	if filterEmpty {
		whereClause = "WHERE value IS NOT NULL AND value != ''"
	}

	query := fmt.Sprintf(`
		SELECT value, COUNT(*) as count
		FROM attestations,
		json_each(%s)
		%s
		GROUP BY value
		ORDER BY count DESC
		LIMIT %d
	`, column, whereClause, MaxSymbolQueryResults)

	rows, err := idx.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[string]int)
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		results[value] = count
	}

	return results, rows.Err()
}

// querySymbols extracts all values from a JSON array column
func (idx *SymbolIndex) querySymbols(ctx context.Context, column string) (map[string]int, error) {
	return idx.queryJSONArrayColumn(ctx, column, false)
}

// queryActors extracts actors from JSON array in attestations
func (idx *SymbolIndex) queryActors(ctx context.Context) (map[string]int, error) {
	return idx.queryJSONArrayColumn(ctx, "actors", true)
}

// GetAttestationCount returns count for a symbol
func (idx *SymbolIndex) GetAttestationCount(symbol, symbolType string) int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	switch symbolType {
	case "subject":
		return idx.subjects[symbol]
	case "predicate":
		return idx.predicates[symbol]
	case "context":
		return idx.contexts[symbol]
	case "actor":
		return idx.actors[symbol]
	default:
		return 0
	}
}

// GetSubjectCompletions returns subject suggestions matching prefix
// Uses 3-char minimum since subjects appear at query start (ambiguous context)
func (idx *SymbolIndex) GetSubjectCompletions(prefix string) []types.CompletionItem {
	return idx.getCompletions(prefix, idx.subjects, "subject", 3)
}

// GetPredicateCompletions returns predicate suggestions
// Uses 1-char minimum since predicates follow "is" keyword (explicit context)
func (idx *SymbolIndex) GetPredicateCompletions(prefix string) []types.CompletionItem {
	return idx.getCompletions(prefix, idx.predicates, "predicate", 1)
}

// GetContextCompletions returns context suggestions
// Uses 1-char minimum since contexts follow "of" keyword (explicit context)
func (idx *SymbolIndex) GetContextCompletions(prefix string) []types.CompletionItem {
	return idx.getCompletions(prefix, idx.contexts, "context", 1)
}

// GetActorCompletions returns actor suggestions
// Uses 1-char minimum since actors follow "by" keyword (explicit context)
func (idx *SymbolIndex) GetActorCompletions(prefix string) []types.CompletionItem {
	return idx.getCompletions(prefix, idx.actors, "actor", 1)
}

// getCompletions filters and ranks symbols matching prefix
// minLength determines minimum prefix length (1 for explicit context, 3 for ambiguous)
func (idx *SymbolIndex) getCompletions(prefix string, symbols map[string]int, kind string, minLength int) []types.CompletionItem {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	prefix = strings.ToLower(prefix)
	items := []types.CompletionItem{}

	// Check minimum prefix length based on context
	// Explicit context (after is/of/by): minLength=1
	// Ambiguous context (subjects at start): minLength=3 (reserved for keywords)
	if len(prefix) < minLength {
		return items
	}

	for symbol, count := range symbols {
		if strings.HasPrefix(strings.ToLower(symbol), prefix) {
			items = append(items, types.CompletionItem{
				Label:      symbol,
				Kind:       kind,
				InsertText: symbol,
				Detail:     fmt.Sprintf("%d attestations", count),
				SortText:   fmt.Sprintf("%04d", 9999-count), // Higher count = lower sort value
			})
		}
	}

	// TODO(QNTX #45): Implement proper sorting by frequency instead of truncation
	// Current behavior: truncates to first 10 matches (unsorted)
	// Desired behavior: sort by count DESC, then alphabetically, then return top 10
	// This requires sort.Slice() on items using SortText field
	if len(items) > 10 {
		items = items[:10]
	}

	return items
}

// LastUpdate returns when the index was last refreshed
func (idx *SymbolIndex) LastUpdate() time.Time {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.lastUpdate
}
