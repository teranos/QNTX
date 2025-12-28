package graph

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// TestExpandAttestation tests the cartesian product expansion
func TestExpandAttestation(t *testing.T) {
	attestation := types.As{
		Subjects:   []string{"alice", "bob"},
		Predicates: []string{"is", "occupation"},
		Contexts:   []string{"engineer", "developer"},
		Actors:     []string{"system"},
		Timestamp:  time.Now(),
	}

	claims := expandAttestation(attestation)

	// Should produce 2 subjects × 2 predicates × 2 contexts = 8 claims
	expectedCount := 8
	if len(claims) != expectedCount {
		t.Errorf("expandAttestation produced %d claims, want %d", len(claims), expectedCount)
	}

	// Verify all combinations exist
	expectedCombinations := []struct {
		subject   string
		predicate string
		context   string
	}{
		{"alice", "is", "engineer"},
		{"alice", "is", "developer"},
		{"alice", "occupation", "engineer"},
		{"alice", "occupation", "developer"},
		{"bob", "is", "engineer"},
		{"bob", "is", "developer"},
		{"bob", "occupation", "engineer"},
		{"bob", "occupation", "developer"},
	}

	for _, expected := range expectedCombinations {
		found := false
		for _, claim := range claims {
			if claim.Subject == expected.subject &&
				claim.Predicate == expected.predicate &&
				claim.Context == expected.context {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected claim not found: %s %s %s", expected.subject, expected.predicate, expected.context)
		}
	}
}

// TestExpandAttestationEmpty tests empty attestation expansion
func TestExpandAttestationEmpty(t *testing.T) {
	tests := []struct {
		name        string
		attestation types.As
		expectedLen int
	}{
		{
			name: "empty_subjects",
			attestation: types.As{
				Subjects:   []string{},
				Predicates: []string{"is"},
				Contexts:   []string{"engineer"},
			},
			expectedLen: 0,
		},
		{
			name: "empty_predicates",
			attestation: types.As{
				Subjects:   []string{"alice"},
				Predicates: []string{},
				Contexts:   []string{"engineer"},
			},
			expectedLen: 0,
		},
		{
			name: "empty_contexts",
			attestation: types.As{
				Subjects:   []string{"alice"},
				Predicates: []string{"is"},
				Contexts:   []string{},
			},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := expandAttestation(tt.attestation)
			if len(claims) != tt.expectedLen {
				t.Errorf("expandAttestation produced %d claims, want %d", len(claims), tt.expectedLen)
			}
		})
	}
}
