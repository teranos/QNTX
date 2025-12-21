package ax

import (
	"github.com/sbvh/qntx/ats"
	"github.com/sbvh/qntx/ats/types"
)

// DetectBasicConflicts performs simple conflict detection
// This is a simplified version - we defer complex conflict analysis for later phases
func DetectBasicConflicts(claims []ats.IndividualClaim) []types.Conflict {
	groups := ats.GroupClaimsByKey(claims)
	conflicts := []types.Conflict{}

	for key, claimsGroup := range groups {
		// Simple conflict detection: if we have multiple claims for the same (S,P,C)
		// and they have different predicates from different source attestations, it might be a conflict
		if len(claimsGroup) > 1 && hasDifferentSources(claimsGroup) {
			conflict := types.Conflict{
				Subject:      key.Subject,
				Predicate:    key.Predicate,
				Context:      key.Context,
				Attestations: getUniqueSourceAttestations(claimsGroup),
				Resolution:   "potential_conflict", // Simple classification for now
			}
			conflicts = append(conflicts, conflict)
		}
	}

	return conflicts
}

// hasDifferentSources checks if claims come from different source attestations
func hasDifferentSources(claims []ats.IndividualClaim) bool {
	if len(claims) <= 1 {
		return false
	}

	firstSourceID := claims[0].SourceAs.ID
	for _, claim := range claims[1:] {
		if claim.SourceAs.ID != firstSourceID {
			return true
		}
	}
	return false
}

// getUniqueSourceAttestations extracts unique source attestations from claims
func getUniqueSourceAttestations(claims []ats.IndividualClaim) []types.As {
	seen := make(map[string]bool)
	attestations := []types.As{}

	for _, claim := range claims {
		if !seen[claim.SourceAs.ID] {
			seen[claim.SourceAs.ID] = true
			attestations = append(attestations, claim.SourceAs)
		}
	}

	return attestations
}
