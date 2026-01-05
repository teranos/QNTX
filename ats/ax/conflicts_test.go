package ax

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

func TestBasicConflictDetection(t *testing.T) {
	now := time.Now()

	as1 := types.As{ID: "AS1", Subjects: []string{"ALICE"}}
	as2 := types.As{ID: "AS2", Subjects: []string{"ALICE"}}

	// Create claims that should conflict (same S,P,C but different sources)
	claims := []ats.IndividualClaim{
		{
			Subject: "ALICE", Predicate: "developer", Context: "ACME",
			Actor: "hr", Timestamp: now, SourceAs: as1,
		},
		{
			Subject: "ALICE", Predicate: "developer", Context: "ACME",
			Actor: "registry", Timestamp: now, SourceAs: as2,
		},
	}

	conflicts := DetectBasicConflicts(claims)

	// Should detect one conflict
	assert.Equal(t, 1, len(conflicts))

	conflict := conflicts[0]
	assert.Equal(t, "ALICE", conflict.Subject)
	assert.Equal(t, "developer", conflict.Predicate)
	assert.Equal(t, "ACME", conflict.Context)
	assert.Equal(t, "potential_conflict", conflict.Resolution)
	assert.Equal(t, 2, len(conflict.Attestations))
}

func TestNoConflictSameSource(t *testing.T) {
	now := time.Now()

	as1 := types.As{ID: "AS1", Subjects: []string{"ALICE"}}

	// Create claims from same source (no conflict)
	claims := []ats.IndividualClaim{
		{
			Subject: "ALICE", Predicate: "developer", Context: "ACME",
			Actor: "hr", Timestamp: now, SourceAs: as1,
		},
		{
			Subject: "ALICE", Predicate: "engineer", Context: "ACME",
			Actor: "hr", Timestamp: now, SourceAs: as1,
		},
	}

	conflicts := DetectBasicConflicts(claims)

	// Should not detect conflicts (same source)
	assert.Equal(t, 0, len(conflicts))
}

func TestNoConflictDifferentClaims(t *testing.T) {
	now := time.Now()

	as1 := types.As{ID: "AS1", Subjects: []string{"ALICE"}}
	as2 := types.As{ID: "AS2", Subjects: []string{"BOB"}}

	// Create completely different claims (no conflict)
	claims := []ats.IndividualClaim{
		{
			Subject: "ALICE", Predicate: "developer", Context: "ACME",
			Actor: "hr", Timestamp: now, SourceAs: as1,
		},
		{
			Subject: "BOB", Predicate: "manager", Context: "CORP",
			Actor: "registry", Timestamp: now, SourceAs: as2,
		},
	}

	conflicts := DetectBasicConflicts(claims)

	// Should not detect conflicts (different claims)
	assert.Equal(t, 0, len(conflicts))
}

func TestHasDifferentSources(t *testing.T) {
	as1 := types.As{ID: "AS1"}
	as2 := types.As{ID: "AS2"}

	// Test same source
	sameClaims := []ats.IndividualClaim{
		{SourceAs: as1},
		{SourceAs: as1},
	}
	assert.False(t, hasDifferentSources(sameClaims))

	// Test different sources
	diffClaims := []ats.IndividualClaim{
		{SourceAs: as1},
		{SourceAs: as2},
	}
	assert.True(t, hasDifferentSources(diffClaims))

	// Test single claim
	singleClaim := []ats.IndividualClaim{
		{SourceAs: as1},
	}
	assert.False(t, hasDifferentSources(singleClaim))
}

func TestGetUniqueSourceAttestations(t *testing.T) {
	as1 := types.As{ID: "AS1", Subjects: []string{"ALICE"}}
	as2 := types.As{ID: "AS2", Subjects: []string{"BOB"}}

	claims := []ats.IndividualClaim{
		{SourceAs: as1},
		{SourceAs: as1}, // Duplicate
		{SourceAs: as2},
	}

	attestations := getUniqueSourceAttestations(claims)

	// Should deduplicate
	assert.Equal(t, 2, len(attestations))

	ids := []string{}
	for _, as := range attestations {
		ids = append(ids, as.ID)
	}

	assert.Contains(t, ids, "AS1")
	assert.Contains(t, ids, "AS2")
}
