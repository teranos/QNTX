package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// SQLQueryStore implements ats.AttestationQueryStore for SQL databases
type SQLQueryStore struct {
	db            *sql.DB
	queryExpander ats.QueryExpander // Optional query expander for NL queries
}

// NewSQLQueryStore creates a new SQL query store
func NewSQLQueryStore(db *sql.DB) *SQLQueryStore {
	return &SQLQueryStore{
		db:            db,
		queryExpander: &ats.NoOpQueryExpander{}, // Default to no expansion
	}
}

// NewSQLQueryStoreWithExpander creates a SQL query store with custom QueryExpander
func NewSQLQueryStoreWithExpander(db *sql.DB, expander ats.QueryExpander) *SQLQueryStore {
	return &SQLQueryStore{
		db:            db,
		queryExpander: expander,
	}
}

// GetAllPredicates returns all distinct predicates in the database
func (s *SQLQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT predicates
		FROM attestations
		WHERE predicates != '["_"]' AND predicates != '[]'
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		err = errors.Wrap(err, "failed to query predicates")
		err = errors.WithDetail(err, "Operation: GetAllPredicates")
		return nil, err
	}
	defer rows.Close()

	var allPredicates []string
	seenPredicates := make(map[string]bool)

	for rows.Next() {
		var predicatesJSON string
		if err := rows.Scan(&predicatesJSON); err != nil {
			err = errors.Wrap(err, "failed to scan predicates")
			err = errors.WithDetail(err, "Operation: GetAllPredicates")
			return nil, err
		}

		// Simple JSON parsing - extract predicates from JSON array
		predicates := parsePredicatesFromJSON(predicatesJSON)
		for _, predicate := range predicates {
			if predicate != "_" && predicate != "" && !seenPredicates[predicate] {
				allPredicates = append(allPredicates, predicate)
				seenPredicates[predicate] = true
			}
		}
	}

	return allPredicates, rows.Err()
}

// GetAllContexts returns all distinct contexts in the database
func (s *SQLQueryStore) GetAllContexts(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT contexts
		FROM attestations
		WHERE contexts != '["_"]' AND contexts != '[]'
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		err = errors.Wrap(err, "failed to query contexts")
		err = errors.WithDetail(err, "Operation: GetAllContexts")
		return nil, err
	}
	defer rows.Close()

	var allContexts []string
	seenContexts := make(map[string]bool)

	for rows.Next() {
		var contextsJSON string
		if err := rows.Scan(&contextsJSON); err != nil {
			err = errors.Wrap(err, "failed to scan contexts")
			err = errors.WithDetail(err, "Operation: GetAllContexts")
			return nil, err
		}

		// Simple JSON parsing - extract contexts from JSON array
		contexts := parsePredicatesFromJSON(contextsJSON) // Reuse the JSON parser
		for _, context := range contexts {
			if context != "_" && context != "" && !seenContexts[context] {
				allContexts = append(allContexts, context)
				seenContexts[context] = true
			}
		}
	}

	return allContexts, rows.Err()
}

// ExecuteAxQuery executes an ax filter query and returns matching attestations
func (s *SQLQueryStore) ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error) {
	// Build WHERE clauses using query builder
	qb := &queryBuilder{}

	// Add subject filter
	qb.buildSubjectFilter(filter.Subjects)

	// Handle natural language vs standard queries
	if s.isNaturalLanguageQuery(filter) {
		qb.buildNaturalLanguageFilter(s.queryExpander, filter)
	} else {
		qb.buildPredicateFilter(filter.Predicates)
		qb.buildContextFilter(filter.Contexts)
	}

	// Add actor filter
	qb.buildActorFilter(filter.Actors)

	// Add numeric comparison filter (OVER) - now with temporal aggregation support
	if err := qb.buildOverComparisonFilter(s.queryExpander, filter.OverComparison, len(qb.whereClauses) > 0, filter); err != nil {
		return nil, errors.Wrap(err, "failed to build over comparison filter")
	}

	// Add temporal filters (attestation timestamp)
	qb.buildTemporalFilters(filter)

	// Add metadata temporal filters to main query when using temporal aggregation
	// This ensures we only return attestations within the time window, not all attestations for matching subjects
	if filter.OverComparison != nil && (filter.TimeStart != nil || filter.TimeEnd != nil) {
		qb.buildMetadataTemporalFilters(filter)
	}

	// Build full query
	query := AttestationSelectQuery
	if len(qb.whereClauses) > 0 {
		query += " WHERE " + strings.Join(qb.whereClauses, " AND ")
	}
	query += " ORDER BY timestamp DESC"

	// Apply limit
	if filter.Limit > 0 {
		limit := filter.Limit
		if limit > MaxAttestationLimit {
			limit = MaxAttestationLimit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, qb.args...)
	if err != nil {
		err = errors.Wrap(err, "failed to execute query")
		err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", filter.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", filter.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", filter.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", filter.Actors))
		err = errors.WithDetail(err, fmt.Sprintf("Limit: %d", filter.Limit))
		err = errors.WithDetail(err, "Operation: ExecuteAxQuery")
		return nil, err
	}
	defer rows.Close()

	// Scan results
	var attestations []*types.As
	for rows.Next() {
		as, err := ScanAttestation(rows)
		if err != nil {
			err = errors.Wrap(err, "failed to scan attestation")
			err = errors.WithDetail(err, fmt.Sprintf("Query subjects: %v", filter.Subjects))
			err = errors.WithDetail(err, fmt.Sprintf("Results so far: %d", len(attestations)))
			err = errors.WithDetail(err, "Operation: ExecuteAxQuery scanning")
			return nil, err
		}
		attestations = append(attestations, as)
	}

	return attestations, rows.Err()
}

// parsePredicatesFromJSON extracts predicates from a JSON array string
func parsePredicatesFromJSON(jsonStr string) []string {
	// Remove brackets and quotes, split by comma
	jsonStr = strings.Trim(jsonStr, "[]")
	if jsonStr == "" {
		return []string{}
	}

	parts := strings.Split(jsonStr, ",")
	var predicates []string

	for _, part := range parts {
		// Remove quotes and whitespace
		predicate := strings.Trim(strings.Trim(part, " "), "\"")
		if predicate != "" {
			predicates = append(predicates, predicate)
		}
	}

	return predicates
}

// isNaturalLanguageQuery detects if filter requires QueryExpander processing
func (s *SQLQueryStore) isNaturalLanguageQuery(filter types.AxFilter) bool {
	if len(filter.Predicates) == 0 {
		return false
	}

	// Check if first predicate is a natural language trigger
	firstPred := filter.Predicates[0]
	nlPredicates := s.queryExpander.GetNaturalLanguagePredicates()

	for _, nlPred := range nlPredicates {
		if firstPred == nlPred {
			// NL predicate detected - check if we have semantic values
			return len(filter.Predicates) > 1
		}
	}

	return false
}

// Ensure SQLQueryStore implements the interface
var _ ats.AttestationQueryStore = (*SQLQueryStore)(nil)
