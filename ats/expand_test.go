package ats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats/types"
)

func TestBasicCartesianExpansion(t *testing.T) {
	// Create a multi-dimensional attestation demonstrating Cartesian expansion
	// Characters can operate in multiple factions across multiple locations
	as := types.As{
		ID:         "SW001",
		Subjects:   []string{"LUKE", "LEIA"},
		Predicates: []string{"operates_in", "located_at"},
		Contexts:   []string{"REBELLION", "TATOOINE"},
		Actors:     []string{"imperial-records"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	attestations := []types.As{as}
	claims := ExpandCartesianClaims(attestations)

	// Should create 2×2×2×1 = 8 individual claims
	assert.Equal(t, 8, len(claims))

	// Verify all combinations are present
	expectedCombinations := []struct {
		subject, predicate, context string
	}{
		{"LUKE", "operates_in", "REBELLION"},
		{"LUKE", "operates_in", "TATOOINE"},
		{"LUKE", "located_at", "REBELLION"},
		{"LUKE", "located_at", "TATOOINE"},
		{"LEIA", "operates_in", "REBELLION"},
		{"LEIA", "operates_in", "TATOOINE"},
		{"LEIA", "located_at", "REBELLION"},
		{"LEIA", "located_at", "TATOOINE"},
	}

	for _, expected := range expectedCombinations {
		found := false
		for _, claim := range claims {
			if claim.Subject == expected.subject &&
				claim.Predicate == expected.predicate &&
				claim.Context == expected.context {
				found = true
				// Verify other fields are correctly copied
				assert.Equal(t, "imperial-records", claim.Actor)
				assert.Equal(t, "SW001", claim.SourceAs.ID)
				break
			}
		}
		assert.True(t, found, "Expected combination not found: %+v", expected)
	}
}

func TestSingleDimensionAttestation(t *testing.T) {
	// Create a simple single-dimension attestation
	as := types.As{
		ID:         "SW002",
		Subjects:   []string{"YODA"},
		Predicates: []string{"trained_by"},
		Contexts:   []string{"JEDI-ORDER"},
		Actors:     []string{"jedi-archives"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	attestations := []types.As{as}
	claims := ExpandCartesianClaims(attestations)

	// Should create 1×1×1×1 = 1 claim
	assert.Equal(t, 1, len(claims))

	claim := claims[0]
	assert.Equal(t, "YODA", claim.Subject)
	assert.Equal(t, "trained_by", claim.Predicate)
	assert.Equal(t, "JEDI-ORDER", claim.Context)
	assert.Equal(t, "jedi-archives", claim.Actor)
	assert.Equal(t, "SW002", claim.SourceAs.ID)
}

func TestGroupClaimsByKey(t *testing.T) {
	claims := []IndividualClaim{
		{Subject: "HAN", Predicate: "smuggler", Context: "MILLENNIUM-FALCON", Actor: "rebel-intelligence"},
		{Subject: "HAN", Predicate: "smuggler", Context: "MILLENNIUM-FALCON", Actor: "imperial-bounty"},
		{Subject: "VADER", Predicate: "commands", Context: "DEATH-STAR", Actor: "imperial-records"},
	}

	groups := GroupClaimsByKey(claims)

	// Should have 2 groups
	assert.Equal(t, 2, len(groups))

	// Check first group (HAN|smuggler|MILLENNIUM-FALCON)
	hanKey := ClaimKey{Subject: "HAN", Predicate: "smuggler", Context: "MILLENNIUM-FALCON"}
	hanGroup, exists := groups[hanKey]
	assert.True(t, exists)
	assert.Equal(t, 2, len(hanGroup))

	// Check second group (VADER|commands|DEATH-STAR)
	vaderKey := ClaimKey{Subject: "VADER", Predicate: "commands", Context: "DEATH-STAR"}
	vaderGroup, exists := groups[vaderKey]
	assert.True(t, exists)
	assert.Equal(t, 1, len(vaderGroup))
}

func TestConvertClaimsToAttestations(t *testing.T) {
	as1 := types.As{ID: "SW003", Subjects: []string{"LEIA"}}
	as2 := types.As{ID: "SW004", Subjects: []string{"OBI-WAN"}}

	claims := []IndividualClaim{
		{Subject: "LEIA", SourceAs: as1},
		{Subject: "LEIA", SourceAs: as1}, // Duplicate source
		{Subject: "OBI-WAN", SourceAs: as2},
	}

	attestations := ConvertClaimsToAttestations(claims)

	// Should deduplicate by source attestation ID
	assert.Equal(t, 2, len(attestations))

	ids := []string{}
	for _, as := range attestations {
		ids = append(ids, as.ID)
	}

	assert.Contains(t, ids, "SW003")
	assert.Contains(t, ids, "SW004")
}
