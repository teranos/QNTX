package storage

import (
	"fmt"
	"strings"

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

// buildSubjectFilter creates LIKE clauses for subject matching (OR logic)
func (qb *queryBuilder) buildSubjectFilter(subjects []string) {
	if len(subjects) == 0 {
		return
	}

	var subjectClauses []string
	for _, subject := range subjects {
		subjectClauses = append(subjectClauses, "subjects LIKE ? ESCAPE '\\'")
		qb.args = append(qb.args, "%\""+escapeLikePattern(subject)+"\"%")
	}
	qb.whereClauses = append(qb.whereClauses, "("+strings.Join(subjectClauses, " OR ")+")")
}

// buildPredicateFilter creates LIKE clauses for predicate matching (OR logic)
func (qb *queryBuilder) buildPredicateFilter(predicates []string) {
	if len(predicates) == 0 {
		return
	}

	var predicateClauses []string
	for _, predicate := range predicates {
		predicateClauses = append(predicateClauses, "predicates LIKE ? ESCAPE '\\'")
		qb.args = append(qb.args, "%\""+escapeLikePattern(predicate)+"\"%")
	}
	qb.whereClauses = append(qb.whereClauses, "("+strings.Join(predicateClauses, " OR ")+")")
}

// buildContextFilter creates LIKE clauses for context matching (OR logic)
func (qb *queryBuilder) buildContextFilter(contexts []string) {
	if len(contexts) == 0 {
		return
	}

	var contextClauses []string
	for _, context := range contexts {
		contextClauses = append(contextClauses, "contexts LIKE ? COLLATE NOCASE ESCAPE '\\'")
		qb.args = append(qb.args, "%\""+escapeLikePattern(context)+"\"%")
	}
	qb.whereClauses = append(qb.whereClauses, "("+strings.Join(contextClauses, " OR ")+")")
}

// buildActorFilter creates LIKE clauses for actor matching (OR logic)
func (qb *queryBuilder) buildActorFilter(actors []string) {
	if len(actors) == 0 {
		return
	}

	var actorClauses []string
	for _, actor := range actors {
		actorClauses = append(actorClauses, "actors LIKE ? ESCAPE '\\'")
		qb.args = append(qb.args, "%\""+escapeLikePattern(actor)+"\"%")
	}
	qb.whereClauses = append(qb.whereClauses, "("+strings.Join(actorClauses, " OR ")+")")
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
func (qb *queryBuilder) buildOverComparisonFilter(expander ats.QueryExpander, overComparison *types.OverFilter, hasOtherClauses bool) {
	if overComparison == nil {
		return
	}

	// Convert threshold to years
	threshold := overComparison.Value
	if overComparison.Unit == "m" {
		threshold = overComparison.Value / 12.0
	}

	// Get numeric predicates from query expander (domain-specific)
	numericPredicates := expander.GetNumericPredicates()
	if len(numericPredicates) == 0 {
		return
	}

	if hasOtherClauses {
		// Combined query: Use subquery to find subjects with numeric values >= threshold
		predicateConditions := make([]string, len(numericPredicates))
		for i, pred := range numericPredicates {
			// Use parameterized query to prevent SQL injection
			predicateConditions[i] = "json_extract(predicates, '$[0]') = ?"
			qb.args = append(qb.args, pred)
		}

		numericSubquery := fmt.Sprintf(`
			SELECT DISTINCT json_extract(subjects, '$[0]') as subject_id
			FROM attestations
			WHERE (%s)
			AND CAST(json_extract(contexts, '$[0]') AS REAL) >= ?
		`, strings.Join(predicateConditions, " OR "))

		// Add as an additional condition
		numericCondition := "json_extract(subjects, '$[0]') IN (" + numericSubquery + ")"
		qb.whereClauses = append(qb.whereClauses, numericCondition)
		qb.args = append(qb.args, threshold)
	} else {
		// Pure OVER query: Filter directly by numeric predicates
		predicateConditions := make([]string, len(numericPredicates))
		for i, pred := range numericPredicates {
			// Use parameterized query to prevent SQL injection
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
