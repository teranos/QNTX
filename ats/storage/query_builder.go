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

// buildFieldFilter creates LIKE clauses for matching values in a JSON column (OR logic).
// If caseInsensitive is true, adds COLLATE NOCASE to the LIKE clause.
func (qb *queryBuilder) buildFieldFilter(column string, values []string, caseInsensitive bool) {
	if len(values) == 0 {
		return
	}

	likeExpr := column + " LIKE ? ESCAPE '\\'"
	if caseInsensitive {
		likeExpr = column + " LIKE ? COLLATE NOCASE ESCAPE '\\'"
	}

	clauses := make([]string, len(values))
	for i, v := range values {
		clauses[i] = likeExpr
		qb.args = append(qb.args, "%\""+escapeLikePattern(v)+"\"%")
	}
	qb.whereClauses = append(qb.whereClauses, "("+strings.Join(clauses, " OR ")+")")
}

func (qb *queryBuilder) buildSubjectFilter(subjects []string) {
	qb.buildFieldFilter("subjects", subjects, false)
}

func (qb *queryBuilder) buildPredicateFilter(predicates []string) {
	qb.buildFieldFilter("predicates", predicates, false)
}

func (qb *queryBuilder) buildContextFilter(contexts []string) {
	qb.buildFieldFilter("contexts", contexts, true)
}

func (qb *queryBuilder) buildActorFilter(actors []string) {
	qb.buildFieldFilter("actors", actors, false)
}

// escapeLikePattern escapes special characters in LIKE patterns for SQL ESCAPE clause
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
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
			escapedPred := escapeLikePattern(exp.Predicate)
			escapedCtx := escapeLikePattern(exp.Context)

			expansionClauses = append(expansionClauses,
				"(predicates LIKE ? ESCAPE '\\' AND contexts LIKE ? COLLATE NOCASE ESCAPE '\\')")
			qb.args = append(qb.args, "%\""+escapedPred+"\"%", "%\""+escapedCtx+"\"%")
		}
		// Wrap all expansions in OR
		qb.whereClauses = append(qb.whereClauses, "("+strings.Join(expansionClauses, " OR ")+")")
	}

	// Also add any explicit contexts from filter.Contexts
	if len(filter.Contexts) > 0 {
		for _, context := range filter.Contexts {
			qb.whereClauses = append(qb.whereClauses, "contexts LIKE ?")
			qb.args = append(qb.args, "%\""+context+"\"%")
		}
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
	// Use RFC3339 format for full timestamp comparison
	temporalFilter := ""
	if filter.TimeStart != nil {
		temporalFilter = " AND json_extract(attributes, '$.start_time') >= ?"
		qb.args = append(qb.args, filter.TimeStart.Format(time.RFC3339))
	}
	if filter.TimeEnd != nil {
		temporalFilter += " AND json_extract(attributes, '$.start_time') <= ?"
		qb.args = append(qb.args, filter.TimeEnd.Format(time.RFC3339))
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
func (qb *queryBuilder) buildTemporalFilters(filter types.AxFilter) {
	// TimeStart is exclusive (>) to exclude exact boundary matches
	if filter.TimeStart != nil {
		qb.addClause("timestamp > ?", filter.TimeStart)
	}

	// TimeEnd is inclusive (<=) to include items up to and including the end time
	if filter.TimeEnd != nil {
		qb.addClause("timestamp <= ?", filter.TimeEnd)
	}
}

// buildMetadataTemporalFilters adds temporal filters based on metadata fields (start_time/end_time)
// This filters activities by when they occurred, not when attestations were created.
// Used for temporal aggregation queries like "since last 10 years" on experience/activity data.
func (qb *queryBuilder) buildMetadataTemporalFilters(filter types.AxFilter) {
	// Use RFC3339 format for full timestamp comparison, matching the aggregation subquery
	if filter.TimeStart != nil {
		qb.addClause("json_extract(attributes, '$.start_time') >= ?", filter.TimeStart.Format(time.RFC3339))
	}

	if filter.TimeEnd != nil {
		qb.addClause("json_extract(attributes, '$.start_time') <= ?", filter.TimeEnd.Format(time.RFC3339))
	}
}
