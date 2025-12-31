package ax

import (
	"fmt"

	"github.com/teranos/QNTX/ats/types"
)

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
