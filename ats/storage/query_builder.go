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

// buildOverComparisonFilter handles numeric comparison queries (OVER duration)
// Supports temporal aggregation: sums duration values across multiple attestations per subject
func (qb *queryBuilder) buildOverComparisonFilter(expander ats.QueryExpander, overComparison *types.OverFilter, hasOtherClauses bool, filter types.AxFilter) {
	if overComparison == nil {
		return
	}

	// Get numeric predicates from query expander (domain-specific)
	numericPredicates := expander.GetNumericPredicates()
	if len(numericPredicates) == 0 {
		return
	}

	// Check if this is a duration aggregation predicate
	// Supports: seconds (s/sec/seconds), minutes (m/min/minutes), hours (h/hr/hours), months, years (y/yr/years)
	isDurationAggregation := false
	for _, pred := range numericPredicates {
		if strings.Contains(pred, "_duration_") {
			isDurationAggregation = true
			break
		}
	}

	if !isDurationAggregation {
		// Legacy behavior: single-value comparison for non-duration predicates
		qb.buildLegacyOverComparison(numericPredicates, overComparison, hasOtherClauses)
		return
	}

	// Temporal aggregation: SUM durations across multiple attestations per subject
	// Determine threshold in the appropriate unit
	var threshold float64
	var durationField string

	// Determine duration unit from predicate suffix
	// Normalize to standard field names: duration_s, duration_minutes, duration_hours, duration_months, duration_years
	usesSeconds := false
	usesMinutes := false
	usesHours := false
	usesYears := false

	for _, pred := range numericPredicates {
		if strings.HasSuffix(pred, "_duration_s") ||
			strings.HasSuffix(pred, "_duration_sec") ||
			strings.HasSuffix(pred, "_duration_seconds") {
			usesSeconds = true
			break
		}
		if strings.HasSuffix(pred, "_duration_m") ||
			strings.HasSuffix(pred, "_duration_min") ||
			strings.HasSuffix(pred, "_duration_minutes") {
			usesMinutes = true
			break
		}
		if strings.HasSuffix(pred, "_duration_h") ||
			strings.HasSuffix(pred, "_duration_hr") ||
			strings.HasSuffix(pred, "_duration_hours") {
			usesHours = true
			break
		}
		if strings.HasSuffix(pred, "_duration_y") ||
			strings.HasSuffix(pred, "_duration_yr") ||
			strings.HasSuffix(pred, "_duration_years") {
			usesYears = true
			break
		}
	}

	if usesSeconds {
		// Duration in seconds
		durationField = "duration_seconds"
		threshold = overComparison.Value
		if overComparison.Unit == "m" {
			threshold = overComparison.Value * 60.0 // minutes to seconds
		} else if overComparison.Unit == "h" {
			threshold = overComparison.Value * 3600.0 // hours to seconds
		}
	} else if usesMinutes {
		// Duration in minutes
		durationField = "duration_minutes"
		threshold = overComparison.Value
		if overComparison.Unit == "s" {
			threshold = overComparison.Value / 60.0 // seconds to minutes
		} else if overComparison.Unit == "h" {
			threshold = overComparison.Value * 60.0 // hours to minutes
		}
	} else if usesHours {
		// Duration in hours
		durationField = "duration_hours"
		threshold = overComparison.Value
		if overComparison.Unit == "m" {
			threshold = overComparison.Value / 60.0 // minutes to hours
		} else if overComparison.Unit == "s" {
			threshold = overComparison.Value / 3600.0 // seconds to hours
		}
	} else if usesYears {
		// Duration in years
		durationField = "duration_years"
		threshold = overComparison.Value
		if overComparison.Unit == "m" {
			threshold = overComparison.Value / 12.0 // months to years
		}
	} else {
		// Duration in months (default)
		durationField = "duration_months"
		threshold = overComparison.Value * 12.0 // years to months
		if overComparison.Unit == "m" {
			threshold = overComparison.Value // already in months
		}
	}

	// Validate durationField against whitelist to prevent SQL injection
	allowedDurationFields := map[string]bool{
		"duration_seconds": true,
		"duration_minutes": true,
		"duration_hours":   true,
		"duration_months":  true,
		"duration_years":   true,
	}
	if !allowedDurationFields[durationField] {
		// This should never happen with current code logic, but guards against future changes
		panic(fmt.Sprintf("invalid duration field: %s (not in whitelist)", durationField))
	}

	// Build predicate conditions for subquery
	predicateConditions := make([]string, len(numericPredicates))
	for i, pred := range numericPredicates {
		predicateConditions[i] = "json_extract(predicates, '$[0]') = ?"
		qb.args = append(qb.args, pred)
	}

	// Build temporal filter for subquery if present
	// Use UTC RFC3339 format to match Rust-stored timestamps
	temporalFilter := ""
	if filter.TimeStart != nil {
		temporalFilter = " AND json_extract(attributes, '$.start_time') >= ?"
		qb.args = append(qb.args, filter.TimeStart.UTC().Format(time.RFC3339))
	}
	if filter.TimeEnd != nil {
		temporalFilter += " AND json_extract(attributes, '$.start_time') <= ?"
		qb.args = append(qb.args, filter.TimeEnd.UTC().Format(time.RFC3339))
	}

	// Aggregation subquery: GROUP BY subject, SUM durations, filter by threshold
	aggregationSubquery := fmt.Sprintf(`
		SELECT json_extract(subjects, '$[0]') as subject_id
		FROM attestations
		WHERE (%s)%s
		GROUP BY json_extract(subjects, '$[0]')
		HAVING SUM(CAST(json_extract(attributes, '$.%s') AS REAL)) >= ?
	`, strings.Join(predicateConditions, " OR "), temporalFilter, durationField)

	// Add as subject filter
	qb.whereClauses = append(qb.whereClauses, "json_extract(subjects, '$[0]') IN ("+aggregationSubquery+")")
	qb.args = append(qb.args, threshold)
}

// buildLegacyOverComparison handles single-value numeric comparisons (non-aggregation)
func (qb *queryBuilder) buildLegacyOverComparison(numericPredicates []string, overComparison *types.OverFilter, hasOtherClauses bool) {
	// Convert threshold to years
	threshold := overComparison.Value
	if overComparison.Unit == "m" {
		threshold = overComparison.Value / 12.0
	}

	if hasOtherClauses {
		// Combined query: Use subquery to find subjects with numeric values >= threshold
		predicateConditions := make([]string, len(numericPredicates))
		for i, pred := range numericPredicates {
			predicateConditions[i] = "json_extract(predicates, '$[0]') = ?"
			qb.args = append(qb.args, pred)
		}

		numericSubquery := fmt.Sprintf(`
			SELECT DISTINCT json_extract(subjects, '$[0]') as subject_id
			FROM attestations
			WHERE (%s)
			AND CAST(json_extract(contexts, '$[0]') AS REAL) >= ?
		`, strings.Join(predicateConditions, " OR "))

		numericCondition := "json_extract(subjects, '$[0]') IN (" + numericSubquery + ")"
		qb.whereClauses = append(qb.whereClauses, numericCondition)
		qb.args = append(qb.args, threshold)
	} else {
		// Pure OVER query: Filter directly by numeric predicates
		predicateConditions := make([]string, len(numericPredicates))
		for i, pred := range numericPredicates {
			predicateConditions[i] = "(json_extract(predicates, '$[0]') = ? AND CAST(json_extract(contexts, '$[0]') AS REAL) >= ?)"
			qb.args = append(qb.args, pred)
			qb.args = append(qb.args, threshold)
		}
		qb.whereClauses = append(qb.whereClauses, "("+strings.Join(predicateConditions, " OR ")+")")
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

// buildMetadataTemporalFilters adds temporal filters based on metadata fields (start_time/end_time)
// This filters activities by when they occurred, not when attestations were created.
// Used for temporal aggregation queries like "since last 10 years" on experience/activity data.
func (qb *queryBuilder) buildMetadataTemporalFilters(filter types.AxFilter) {
	// Use UTC RFC3339 format to match Rust-stored timestamps
	if filter.TimeStart != nil {
		qb.addClause("json_extract(attributes, '$.start_time') >= ?", filter.TimeStart.UTC().Format(time.RFC3339))
	}

	if filter.TimeEnd != nil {
		qb.addClause("json_extract(attributes, '$.start_time') <= ?", filter.TimeEnd.UTC().Format(time.RFC3339))
	}
}
