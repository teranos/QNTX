//go:build !qntxwasm

package ats

import (
	"github.com/teranos/QNTX/ats/types"
)

// ExpandCartesianClaims expands multi-dimensional attestations into individual claims.
// Go fallback — used when the qntxwasm build tag is not set.
func ExpandCartesianClaims(attestations []types.As) []IndividualClaim {
	claims := []IndividualClaim{}

	for _, as := range attestations {
		expandedClaims := expandSingleAttestation(as)
		claims = append(claims, expandedClaims...)
	}

	return claims
}

// expandSingleAttestation expands one attestation into its cartesian product of claims
func expandSingleAttestation(as types.As) []IndividualClaim {
	// Pre-allocate slice with exact capacity to avoid reallocations during cartesian product expansion
	capacity := len(as.Subjects) * len(as.Predicates) * len(as.Contexts) * len(as.Actors)
	claims := make([]IndividualClaim, 0, capacity)

	// Simple cartesian product: subjects × predicates × contexts × actors
	for _, subject := range as.Subjects {
		for _, predicate := range as.Predicates {
			for _, context := range as.Contexts {
				for _, actor := range as.Actors {
					claims = append(claims, IndividualClaim{
						Subject:   subject,
						Predicate: predicate,
						Context:   context,
						Actor:     actor,
						Timestamp: as.Timestamp,
						SourceAs:  as,
					})
				}
			}
		}
	}

	return claims
}

// GroupClaimsByKey groups claims by their (Subject, Predicate, Context) key.
// Go fallback — used when the qntxwasm build tag is not set.
func GroupClaimsByKey(claims []IndividualClaim) map[ClaimKey][]IndividualClaim {
	groups := make(map[ClaimKey][]IndividualClaim)

	for _, claim := range claims {
		key := ClaimKey{
			Subject:   claim.Subject,
			Predicate: claim.Predicate,
			Context:   claim.Context,
		}
		groups[key] = append(groups[key], claim)
	}

	return groups
}

// ConvertClaimsToAttestations converts individual claims back to unique attestations
// for display purposes - removes duplicates by source attestation ID.
// Go fallback — used when the qntxwasm build tag is not set.
func ConvertClaimsToAttestations(claims []IndividualClaim) []types.As {
	seen := make(map[string]bool)
	result := []types.As{}

	for _, claim := range claims {
		if !seen[claim.SourceAs.ID] {
			seen[claim.SourceAs.ID] = true
			result = append(result, claim.SourceAs)
		}
	}

	return result
}
