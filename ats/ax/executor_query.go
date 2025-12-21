package ax

import (
	"fmt"
	"strings"
	"time"

	"github.com/sbvh/qntx/ats/types"
)

// sanitizeLikePattern escapes SQL LIKE pattern metacharacters to prevent injection
// Escapes: % (wildcard), _ (single char wildcard), \ (escape char)
func sanitizeLikePattern(input string) string {
	// Escape backslashes first to avoid double-escaping
	escaped := strings.ReplaceAll(input, "\\", "\\\\")
	// Escape LIKE wildcards
	escaped = strings.ReplaceAll(escaped, "%", "\\%")
	escaped = strings.ReplaceAll(escaped, "_", "\\_")
	return escaped
}

// buildJSONLikePattern safely constructs a LIKE pattern for JSON field searching
// Example: buildJSONLikePattern("user") returns "%\"user\"%"
func buildJSONLikePattern(value string) string {
	sanitized := sanitizeLikePattern(value)
	return "%\"" + sanitized + "\"%"
}

// buildSQLQuery builds a SQL query from the filter
func (ae *AxExecutor) buildSQLQuery(filter types.AxFilter, expandedPredicates []string) (string, []interface{}, error) {
	// Debug: Log filter for debugging natural language queries
	// fmt.Printf("DEBUG: Filter predicates: %v, expandedPredicates: %v\n", filter.Predicates, expandedPredicates)

	query := `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		FROM attestations
		WHERE 1=1
	`
	args := []interface{}{}

	// Build WHERE conditions using simple LIKE queries
	conditions := []string{}

	// Subject filters
	if len(filter.Subjects) > 0 {
		subjectConditions := []string{}
		for _, subject := range filter.Subjects {
			subjectConditions = append(subjectConditions, "subjects LIKE ? ESCAPE '\\'")
			args = append(args, buildJSONLikePattern(subject))
		}
		conditions = append(conditions, "("+strings.Join(subjectConditions, " OR ")+")")
	}

	// Predicate filters with natural language support
	// When we have predicates like "is" + value, check both:
	// 1. Direct predicate match (traditional)
	// 2. Semantic predicate-context pairs

	// Use either expanded or original predicates
	predicatesToUse := expandedPredicates
	if len(predicatesToUse) == 0 && len(filter.Predicates) > 0 {
		predicatesToUse = filter.Predicates
	}

	if len(predicatesToUse) > 0 {
		// Check if this is a natural language query pattern
		if len(filter.Predicates) > 0 {
			firstPred := filter.Predicates[0]

			// Check if this predicate triggers natural language expansion
			nlPredicates := ae.queryExpander.GetNaturalLanguagePredicates()
			isNaturalLanguage := false
			for _, nlp := range nlPredicates {
				if firstPred == nlp {
					isNaturalLanguage = true
					break
				}
			}

			if isNaturalLanguage && len(filter.Predicates) > 1 {
				// Natural language pattern detected - use query expander
				semanticValues := filter.Predicates[1:]

				// For each semantic value, expand using the query expander
				for _, value := range semanticValues {
					expansions := ae.queryExpander.ExpandPredicate(firstPred, []string{value})

					var valueConditions []string
					for _, exp := range expansions {
						condition := "(predicates LIKE ? ESCAPE '\\' AND contexts LIKE ? COLLATE NOCASE ESCAPE '\\')"
						valueConditions = append(valueConditions, condition)
						args = append(args, buildJSONLikePattern(exp.Predicate))
						args = append(args, "%"+sanitizeLikePattern(exp.Context)+"%")
					}

					// Each value gets its own OR group
					if len(valueConditions) > 0 {
						conditions = append(conditions, "("+strings.Join(valueConditions, " OR ")+")")
					}
				}
			} else {
				// Traditional predicate matching (no semantic expansion)
				predicateConditions := []string{}
				for _, predicate := range predicatesToUse {
					predicateConditions = append(predicateConditions, "predicates LIKE ? ESCAPE '\\'")
					args = append(args, buildJSONLikePattern(predicate))
				}
				conditions = append(conditions, "("+strings.Join(predicateConditions, " OR ")+")")
			}
		}
	}

	// Context filters
	if len(filter.Contexts) > 0 {
		contextConditions := []string{}
		for _, context := range filter.Contexts {
			contextConditions = append(contextConditions, "contexts LIKE ? COLLATE NOCASE ESCAPE '\\'")
			args = append(args, buildJSONLikePattern(context))
		}
		conditions = append(conditions, "("+strings.Join(contextConditions, " OR ")+")")
	}

	// Actor filters
	if len(filter.Actors) > 0 {
		actorConditions := []string{}
		for _, actor := range filter.Actors {
			actorConditions = append(actorConditions, "actors LIKE ? ESCAPE '\\'")
			args = append(args, buildJSONLikePattern(actor))
		}
		conditions = append(conditions, "("+strings.Join(actorConditions, " OR ")+")")
	}

	// Temporal filters
	if filter.TimeStart != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.TimeStart.Format(time.RFC3339))
	}

	if filter.TimeEnd != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.TimeEnd.Format(time.RFC3339))
	}

	// Add WHERE conditions
	// Handle OVER filtering for combined queries (e.g., "is engineer over 5y")
	if filter.OverComparison != nil && len(conditions) > 0 {
		// For combined queries, we need to find subjects that satisfy BOTH conditions:
		// 1. The semantic/predicate conditions (already in conditions array)
		// 2. The experience threshold from OverComparison

		// Convert OVER threshold to years
		threshold := filter.OverComparison.Value
		if filter.OverComparison.Unit == "m" {
			threshold = filter.OverComparison.Value / 12.0
		}

		// Build subquery to find subjects with sufficient numeric values
		// Get numeric predicates from expander (domain-specific)
		numericPredicates := ae.queryExpander.GetNumericPredicates()
		if len(numericPredicates) == 0 {
			// If no numeric predicates defined, skip OverComparison filtering
			// (will fall back to post-processing filter)
		} else {
			// Build OR conditions for all numeric predicates
			predicateConditions := make([]string, len(numericPredicates))
			for i, pred := range numericPredicates {
				predicateConditions[i] = fmt.Sprintf("json_extract(predicates, '$[0]') = '%s'", pred)
			}

			numericSubquery := fmt.Sprintf(`
				SELECT DISTINCT json_extract(subjects, '$[0]') as subject_id
				FROM attestations
				WHERE (%s)
				AND CAST(json_extract(contexts, '$[0]') AS REAL) >= ?
			`, strings.Join(predicateConditions, " OR "))

			// Add the numeric condition as an intersection to the conditions array
			numericCondition := `json_extract(subjects, '$[0]') IN (` + numericSubquery + `)`
			conditions = append(conditions, numericCondition)
			args = append(args, threshold)
		}
	}

	if len(conditions) > 0 {
		query += " AND " + strings.Join(conditions, " AND ")
	}

	// Order by timestamp descending (most recent first)
	query += " ORDER BY timestamp DESC"

	// Add LIMIT for basic pagination
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	} else if filter.OverComparison == nil {
		// Default limit to prevent huge result sets for non-OVER queries
		query += " LIMIT 1000"
	} else {
		// For OVER queries, add a safety limit to prevent memory issues
		// OVER filtering requires access to attestations for post-processing,
		// but we need reasonable bounds to prevent OOM on large datasets
		query += " LIMIT 10000"
	}

	return query, args, nil
}

// shouldUsePostProcessingForOver determines whether to use post-processing or SQL for OVER filtering
// Use SQL for combined queries (predicates/contexts + OVER), post-processing for pure OVER queries
func (ae *AxExecutor) shouldUsePostProcessingForOver(filter types.AxFilter) bool {
	// If there are other filter conditions, we handle OVER in SQL
	if len(filter.Subjects) > 0 || len(filter.Predicates) > 0 || len(filter.Contexts) > 0 || len(filter.Actors) > 0 {
		return false // Use SQL approach for combined queries
	}

	// For pure OVER queries (no other conditions), use post-processing
	return true
}

// buildOverConditions builds SQL conditions for "over" temporal comparisons
func (ae *AxExecutor) buildOverConditions(overFilter *types.OverFilter, args *[]interface{}) []string {
	var conditions []string

	// Convert value to years for comparison
	threshold := overFilter.Value
	if overFilter.Unit == "m" {
		// Convert months to years
		threshold = overFilter.Value / 12.0
	}

	// Get experience predicates from query expander (domain-specific)
	experiencePredicates := ae.queryExpander.GetNumericPredicates()

	for _, pred := range experiencePredicates {
		// For numeric comparison, cast the JSON context value
		condition := fmt.Sprintf("(predicates LIKE ? ESCAPE '\\' AND CAST(json_extract(contexts, '$[0]') AS REAL) >= ?)")
		conditions = append(conditions, condition)
		*args = append(*args, buildJSONLikePattern(pred))
		*args = append(*args, threshold)
	}

	return conditions
}

// applyOverFilter applies "over" numeric filtering as post-processing
// This works by finding subjects that have any attestation with experience >= threshold
func (ae *AxExecutor) applyOverFilter(attestations []types.As, overFilter *types.OverFilter) []types.As {
	if overFilter == nil {
		return attestations
	}

	// Group attestations by subject
	subjectGroups := make(map[string][]types.As)
	for _, attestation := range attestations {
		for _, subject := range attestation.Subjects {
			subjectGroups[subject] = append(subjectGroups[subject], attestation)
		}
	}

	// Convert threshold to years for comparison
	threshold := overFilter.Value
	if overFilter.Unit == "m" {
		threshold = overFilter.Value / 12.0
	}

	// Find subjects that meet the experience threshold
	qualifyingSubjects := make(map[string]bool)
	// Get experience predicates from query expander (domain-specific)
	experiencePredicates := ae.queryExpander.GetNumericPredicates()

	for subject, subjectAttestations := range subjectGroups {
		// Check all attestations for this subject
		for _, attestation := range subjectAttestations {
			// Look for experience predicates
			for _, predicate := range attestation.Predicates {
				for _, expPred := range experiencePredicates {
					if predicate == expPred {
						// Try to parse numeric context
						for _, context := range attestation.Contexts {
							if years, err := parseFloatExperience(context); err == nil && years >= threshold {
								qualifyingSubjects[subject] = true
								break
							}
						}
					}
				}
				if qualifyingSubjects[subject] {
					break
				}
			}
			if qualifyingSubjects[subject] {
				break
			}
		}
	}

	// Return only attestations for qualifying subjects
	var filteredAttestations []types.As
	for _, attestation := range attestations {
		includeAttestation := false
		for _, subject := range attestation.Subjects {
			if qualifyingSubjects[subject] {
				includeAttestation = true
				break
			}
		}
		if includeAttestation {
			filteredAttestations = append(filteredAttestations, attestation)
		}
	}

	return filteredAttestations
}

// parseFloatExperience safely parses a string as float for experience values
func parseFloatExperience(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
