//go:build qntxwasm

package ats

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
)

// ExpandCartesianClaims expands multi-dimensional attestations into individual claims
// via the Rust WASM engine.
func ExpandCartesianClaims(attestations []types.As) []IndividualClaim {
	engine, err := wasm.GetEngine()
	if err != nil {
		panic("WASM engine unavailable for expand: " + err.Error() + " — run `make wasm`")
	}

	wasmAttestations := make([]wasm.ExpandAttestationInput, len(attestations))
	for i, as := range attestations {
		wasmAttestations[i] = wasm.ExpandAttestationInput{
			ID:          as.ID,
			Subjects:    as.Subjects,
			Predicates:  as.Predicates,
			Contexts:    as.Contexts,
			Actors:      as.Actors,
			TimestampMs: as.Timestamp.UnixMilli(),
		}
	}

	output, err := engine.ExpandCartesianClaims(wasm.ExpandInput{
		Attestations: wasmAttestations,
	})
	if err != nil {
		panic("WASM expand_cartesian_claims failed: " + err.Error())
	}

	// Build a lookup from attestation ID → original As for SourceAs references
	asLookup := make(map[string]types.As, len(attestations))
	for _, as := range attestations {
		asLookup[as.ID] = as
	}

	claims := make([]IndividualClaim, len(output.Claims))
	for i, c := range output.Claims {
		claims[i] = IndividualClaim{
			Subject:   c.Subject,
			Predicate: c.Predicate,
			Context:   c.Context,
			Actor:     c.Actor,
			Timestamp: time.UnixMilli(c.TimestampMs),
			SourceAs:  asLookup[c.SourceID],
		}
	}

	return claims
}

// GroupClaimsByKey groups claims by their (Subject, Predicate, Context) key
// via the Rust WASM engine.
func GroupClaimsByKey(claims []IndividualClaim) map[ClaimKey][]IndividualClaim {
	engine, err := wasm.GetEngine()
	if err != nil {
		panic("WASM engine unavailable for group_claims: " + err.Error() + " — run `make wasm`")
	}

	wasmClaims := make([]wasm.ExpandClaimOutput, len(claims))
	for i, c := range claims {
		wasmClaims[i] = wasm.ExpandClaimOutput{
			Subject:     c.Subject,
			Predicate:   c.Predicate,
			Context:     c.Context,
			Actor:       c.Actor,
			TimestampMs: c.Timestamp.UnixMilli(),
			SourceID:    c.SourceAs.ID,
		}
	}

	output, err := engine.GroupClaims(wasm.GroupClaimsInput{Claims: wasmClaims})
	if err != nil {
		panic("WASM group_claims failed: " + err.Error())
	}

	// Build claim lookup by source_id+actor for SourceAs reconstruction
	claimLookup := make(map[string]types.As)
	for _, c := range claims {
		claimLookup[c.SourceAs.ID] = c.SourceAs
	}

	groups := make(map[ClaimKey][]IndividualClaim, len(output.Groups))
	for _, g := range output.Groups {
		var key ClaimKey
		// Parse "subject|predicate|context" back to struct
		parts := splitClaimKey(g.Key)
		if len(parts) == 3 {
			key = ClaimKey{Subject: parts[0], Predicate: parts[1], Context: parts[2]}
		}

		groupClaims := make([]IndividualClaim, len(g.Claims))
		for i, c := range g.Claims {
			groupClaims[i] = IndividualClaim{
				Subject:   c.Subject,
				Predicate: c.Predicate,
				Context:   c.Context,
				Actor:     c.Actor,
				Timestamp: time.UnixMilli(c.TimestampMs),
				SourceAs:  claimLookup[c.SourceID],
			}
		}
		groups[key] = groupClaims
	}

	return groups
}

// ConvertClaimsToAttestations converts individual claims back to unique attestations
// for display purposes - removes duplicates by source attestation ID.
// Uses the Rust WASM engine for deduplication.
func ConvertClaimsToAttestations(claims []IndividualClaim) []types.As {
	engine, err := wasm.GetEngine()
	if err != nil {
		panic("WASM engine unavailable for dedup_source_ids: " + err.Error() + " — run `make wasm`")
	}

	wasmClaims := make([]wasm.ExpandClaimOutput, len(claims))
	for i, c := range claims {
		wasmClaims[i] = wasm.ExpandClaimOutput{
			Subject:     c.Subject,
			Predicate:   c.Predicate,
			Context:     c.Context,
			Actor:       c.Actor,
			TimestampMs: c.Timestamp.UnixMilli(),
			SourceID:    c.SourceAs.ID,
		}
	}

	output, err := engine.DedupSourceIDs(wasm.DedupInput{Claims: wasmClaims})
	if err != nil {
		panic("WASM dedup_source_ids failed: " + err.Error())
	}

	// Build lookup for source attestations
	asLookup := make(map[string]types.As)
	for _, c := range claims {
		asLookup[c.SourceAs.ID] = c.SourceAs
	}

	result := make([]types.As, 0, len(output.IDs))
	for _, id := range output.IDs {
		if as, ok := asLookup[id]; ok {
			result = append(result, as)
		}
	}

	return result
}

// splitClaimKey splits a "subject|predicate|context" key into parts.
func splitClaimKey(key string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	parts = append(parts, key[start:])
	return parts
}
