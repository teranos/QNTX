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

// getAllDistinctValues returns all distinct non-placeholder values from a JSON array column
func (s *SQLQueryStore) getAllDistinctValues(ctx context.Context, column string) ([]string, error) {
	query := fmt.Sprintf(`
		SELECT DISTINCT %s
		FROM attestations
		WHERE %s != '["_"]' AND %s != '[]'
	`, column, column, column)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		err = errors.Wrapf(err, "failed to query %s", column)
		err = errors.WithDetail(err, fmt.Sprintf("Operation: GetAll(%s)", column))
		return nil, err
	}
	defer rows.Close()

	var all []string
	seen := make(map[string]bool)

	for rows.Next() {
		var jsonStr string
		if err := rows.Scan(&jsonStr); err != nil {
			err = errors.Wrapf(err, "failed to scan %s", column)
			err = errors.WithDetail(err, fmt.Sprintf("Operation: GetAll(%s)", column))
			return nil, err
		}

		for _, val := range parsePredicatesFromJSON(jsonStr) {
			if val != "_" && val != "" && !seen[val] {
				all = append(all, val)
				seen[val] = true
			}
		}
	}

	return all, rows.Err()
}

// GetAllPredicates returns all distinct predicates in the database
func (s *SQLQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	return s.getAllDistinctValues(ctx, "predicates")
}

// GetAllContexts returns all distinct contexts in the database
func (s *SQLQueryStore) GetAllContexts(ctx context.Context) ([]string, error) {
	return s.getAllDistinctValues(ctx, "contexts")
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
	qb.buildOverComparisonFilter(s.queryExpander, filter.OverComparison, len(qb.whereClauses) > 0, filter)

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
