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

// RawQuerier executes attestation queries through a single connection (Rust FFI).
// When set on SQLQueryStore, all attestation queries route through this instead of *sql.DB.
type RawQuerier interface {
	QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error)
	QueryFilter(filter types.AxFilter) ([]*types.As, error)
	GetAllPredicates() ([]string, error)
	GetAllContexts() ([]string, error)
}

// SQLQueryStore implements ats.AttestationQueryStore for SQL databases
type SQLQueryStore struct {
	db         *sql.DB
	rawQuerier RawQuerier // Optional: routes queries through Rust FFI
}

// NewSQLQueryStore creates a new SQL query store
func NewSQLQueryStore(db *sql.DB) *SQLQueryStore {
	return &SQLQueryStore{db: db}
}

// SetRawQuerier sets the raw query executor (Rust FFI).
// When set, all attestation queries route through this instead of *sql.DB.
func (s *SQLQueryStore) SetRawQuerier(rq RawQuerier) {
	s.rawQuerier = rq
}

// GetAllPredicates returns all distinct predicates in the database
func (s *SQLQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	if s.rawQuerier != nil {
		return s.rawQuerier.GetAllPredicates()
	}

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
	if s.rawQuerier != nil {
		return s.rawQuerier.GetAllContexts()
	}

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

		contexts := parsePredicatesFromJSON(contextsJSON)
		for _, context := range contexts {
			if context != "_" && context != "" && !seenContexts[context] {
				allContexts = append(allContexts, context)
				seenContexts[context] = true
			}
		}
	}

	return allContexts, rows.Err()
}

// ExecuteAxQuery executes an ax filter query and returns matching attestations.
// When Rust FFI is available, the entire query (SQL building + execution) is delegated to Rust.
// The Go query builder is only used as fallback for non-FFI environments (tests).
func (s *SQLQueryStore) ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error) {
	// Route through Rust FFI — Rust builds SQL and executes
	if s.rawQuerier != nil {
		attestations, err := s.rawQuerier.QueryFilter(filter)
		if err != nil {
			err = errors.Wrap(err, "failed to execute query via Rust")
			err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", filter.Subjects))
			err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", filter.Predicates))
			err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", filter.Contexts))
			err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", filter.Actors))
			err = errors.WithDetail(err, fmt.Sprintf("Limit: %d", filter.Limit))
			return nil, err
		}
		return attestations, nil
	}

	// Fallback: Go query builder for non-FFI environments
	qb := &queryBuilder{}
	qb.buildSubjectFilter(filter.Subjects)
	qb.buildPredicateFilter(filter.Predicates)
	qb.buildContextFilter(filter.Contexts)
	qb.buildActorFilter(filter.Actors)
	qb.buildTemporalFilters(filter)

	query := AttestationSelectQuery
	if len(qb.whereClauses) > 0 {
		query += " WHERE " + strings.Join(qb.whereClauses, " AND ")
	}
	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		limit := filter.Limit
		if limit > MaxAttestationLimit {
			limit = MaxAttestationLimit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, qb.args...)
	if err != nil {
		err = errors.Wrap(err, "failed to execute query")
		err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", filter.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", filter.Predicates))
		return nil, err
	}
	defer rows.Close()

	var attestations []*types.As
	for rows.Next() {
		as, err := ScanAttestation(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan attestation")
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

// Ensure SQLQueryStore implements the interface
var _ ats.AttestationQueryStore = (*SQLQueryStore)(nil)
