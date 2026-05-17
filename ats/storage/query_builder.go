package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// queryBuilder accumulates SQL WHERE clauses and parameters for attestation queries
type queryBuilder struct {
	whereClauses []string
	args         []interface{}
}

// addClause appends a WHERE clause with its arguments
func (qb *queryBuilder) addClause(clause string, args ...interface{}) {
	qb.whereClauses = append(qb.whereClauses, clause)
	qb.args = append(qb.args, args...)
}

// build returns the WHERE clauses joined with AND
func (qb *queryBuilder) build() string {
	return strings.Join(qb.whereClauses, " AND ")
}

// buildSubjectFilter uses the attestation_subjects junction table for indexed lookups.
func (qb *queryBuilder) buildSubjectFilter(subjects []string) {
	if len(subjects) == 0 {
		return
	}
	placeholders := make([]string, len(subjects))
	for i, s := range subjects {
		placeholders[i] = "?"
		qb.args = append(qb.args, s)
	}
	qb.whereClauses = append(qb.whereClauses,
		"id IN (SELECT attestation_id FROM attestation_subjects WHERE subject IN ("+strings.Join(placeholders, ",")+"))")
}

// buildPredicateFilter uses the attestation_predicates junction table for indexed lookups.
func (qb *queryBuilder) buildPredicateFilter(predicates []string) {
	if len(predicates) == 0 {
		return
	}
	placeholders := make([]string, len(predicates))
	for i, p := range predicates {
		placeholders[i] = "?"
		qb.args = append(qb.args, p)
	}
	qb.whereClauses = append(qb.whereClauses,
		"id IN (SELECT attestation_id FROM attestation_predicates WHERE predicate IN ("+strings.Join(placeholders, ",")+"))")
}

// buildContextFilter uses the attestation_contexts junction table for indexed lookups.
func (qb *queryBuilder) buildContextFilter(contexts []string) {
	if len(contexts) == 0 {
		return
	}
	placeholders := make([]string, len(contexts))
	for i, c := range contexts {
		placeholders[i] = "?"
		qb.args = append(qb.args, c)
	}
	qb.whereClauses = append(qb.whereClauses,
		"id IN (SELECT attestation_id FROM attestation_contexts WHERE context IN ("+strings.Join(placeholders, ",")+" COLLATE NOCASE))")
}

// buildActorFilter uses the attestation_actors junction table for indexed lookups.
func (qb *queryBuilder) buildActorFilter(actors []string) {
	if len(actors) == 0 {
		return
	}
	placeholders := make([]string, len(actors))
	for i, a := range actors {
		placeholders[i] = "?"
		qb.args = append(qb.args, a)
	}
	qb.whereClauses = append(qb.whereClauses,
		"id IN (SELECT attestation_id FROM attestation_actors WHERE actor IN ("+strings.Join(placeholders, ",")+"))")
}

// BuildFilterQuery builds a SQL query from an AxFilter, returning the full SELECT
// with WHERE clauses and ORDER BY. Used by the watcher engine to push structural
// filters into SQL instead of loading the entire table.
func BuildFilterQuery(filter types.AxFilter) (string, []interface{}) {
	qb := &queryBuilder{}
	qb.buildSubjectFilter(filter.Subjects)
	qb.buildPredicateFilter(filter.Predicates)
	qb.buildContextFilter(filter.Contexts)
	qb.buildActorFilter(filter.Actors)

	if filter.TimeStart != nil {
		qb.addClause("timestamp >= ?", filter.TimeStart.UTC().Format(time.RFC3339))
	}
	if filter.TimeEnd != nil {
		qb.addClause("timestamp <= ?", filter.TimeEnd.UTC().Format(time.RFC3339))
	}

	query := AttestationSelectQuery
	if len(qb.whereClauses) > 0 {
		query += " WHERE " + qb.build()
	}
	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		limit := filter.Limit
		if limit > MaxAttestationLimit {
			limit = MaxAttestationLimit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	return query, qb.args
}

// buildNaturalLanguageFilter handles NL query expansion with predicate-context pairing
func (qb *queryBuilder) buildNaturalLanguageFilter(expander ats.QueryExpander, filter types.AxFilter) {
	if len(filter.Predicates) == 0 {
		return
	}

	// First predicate is the NL predicate, rest are context values
	nlPredicate := filter.Predicates[0]
	contextValues := filter.Predicates[1:]

	// Expand using QueryExpander
	expansions := expander.ExpandPredicate(nlPredicate, contextValues)

	// Build OR clauses for each expansion (predicate AND context pairs)
	if len(expansions) > 0 {
		var expansionClauses []string
		for _, exp := range expansions {
			expansionClauses = append(expansionClauses,
				"(id IN (SELECT attestation_id FROM attestation_predicates WHERE predicate = ?) AND id IN (SELECT attestation_id FROM attestation_contexts WHERE context = ? COLLATE NOCASE))")
			qb.args = append(qb.args, exp.Predicate, exp.Context)
		}
		// Wrap all expansions in OR
		qb.whereClauses = append(qb.whereClauses, "("+strings.Join(expansionClauses, " OR ")+")")
	}

	// Also add any explicit contexts from filter.Contexts
	if len(filter.Contexts) > 0 {
		placeholders := make([]string, len(filter.Contexts))
		for i, ctx := range filter.Contexts {
			placeholders[i] = "?"
			qb.args = append(qb.args, ctx)
		}
		qb.whereClauses = append(qb.whereClauses,
			"id IN (SELECT attestation_id FROM attestation_contexts WHERE context IN ("+strings.Join(placeholders, ",")+" COLLATE NOCASE))")
	}
}

// buildTemporalFilters adds timestamp range filters
// Timestamps are stored as UTC RFC3339 strings by the Rust backend,
// so we compare against UTC RFC3339 formatted strings.
func (qb *queryBuilder) buildTemporalFilters(filter types.AxFilter) {
	// TimeStart is exclusive (>) to exclude exact boundary matches
	if filter.TimeStart != nil {
		qb.addClause("timestamp > ?", filter.TimeStart.UTC().Format(time.RFC3339))
	}

	// TimeEnd is inclusive (<=) to include items up to and including the end time
	if filter.TimeEnd != nil {
		qb.addClause("timestamp <= ?", filter.TimeEnd.UTC().Format(time.RFC3339))
	}
}

