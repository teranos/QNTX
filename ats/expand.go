// Package ats provides cartesian product expansion for attestations.
// It expands compact attestation representations into individual claims for storage.
package ats

import (
	"time"

	"github.com/sbvh/qntx/ats/types"
)

const (
	// ClaimKeySeparator is used to join subject, predicate, and context into a unique key
	ClaimKeySeparator = "|"
)

// IndividualClaim represents a single claim extracted from a multi-dimensional attestation
type IndividualClaim struct {
	Subject   string
	Predicate string
	Context   string
	Actor     string
	Timestamp time.Time
	SourceAs  types.As // Reference to original attestation
}

// ClaimKey represents the unique identifier for a claim (Subject, Predicate, Context)
type ClaimKey struct {
	Subject   string
	Predicate string
	Context   string
}

// String returns a string representation of the claim key
func (ck ClaimKey) String() string {
	return ck.Subject + ClaimKeySeparator + ck.Predicate + ClaimKeySeparator + ck.Context
}

// ExpandCartesianClaims expands multi-dimensional attestations into individual claims
// This is a simplified version - we defer complex filtering and conflict detection for later phases
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

// GroupClaimsByKey groups claims by their (Subject, Predicate, Context) key
// This will be useful for conflict detection in later phases
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
// for display purposes - removes duplicates by source attestation ID
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
