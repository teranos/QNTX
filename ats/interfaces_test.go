package ats_test

import (
	"testing"

	"github.com/teranos/QNTX/ats"
)

// TestNoOpQueryExpander_LiteralMatching verifies the default no-op expander
// returns literal predicate-context pairs without semantic expansion
func TestNoOpQueryExpander_LiteralMatching(t *testing.T) {
	expander := &ats.NoOpQueryExpander{}

	tests := []struct {
		name      string
		predicate string
		values    []string
		wantCount int
	}{
		{
			name:      "single value returns one expansion",
			predicate: "is",
			values:    []string{"engineer"},
			wantCount: 1,
		},
		{
			name:      "multiple values return multiple expansions",
			predicate: "speaks",
			values:    []string{"english", "dutch", "german"},
			wantCount: 3,
		},
		{
			name:      "empty values returns empty",
			predicate: "has",
			values:    []string{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expansions := expander.ExpandPredicate(tt.predicate, tt.values)

			if len(expansions) != tt.wantCount {
				t.Errorf("got %d expansions, want %d", len(expansions), tt.wantCount)
			}

			// Verify each expansion is literal (predicate + context match input)
			for i, exp := range expansions {
				if exp.Predicate != tt.predicate {
					t.Errorf("expansion[%d].Predicate = %q, want %q", i, exp.Predicate, tt.predicate)
				}
				if i < len(tt.values) && exp.Context != tt.values[i] {
					t.Errorf("expansion[%d].Context = %q, want %q", i, exp.Context, tt.values[i])
				}
			}
		})
	}
}

// TestNoOpQueryExpander_NoExperiencePredicates verifies NoOp returns empty
// experience predicates (generic ATS doesn't track time-based queries)
func TestNoOpQueryExpander_NoExperiencePredicates(t *testing.T) {
	expander := &ats.NoOpQueryExpander{}

	predicates := expander.GetNumericPredicates()

	if len(predicates) != 0 {
		t.Errorf("NoOp should return empty experience predicates, got %v", predicates)
	}
}

// TestNoOpQueryExpander_NoNaturalLanguageExpansion verifies NoOp returns empty
// natural language predicates (no semantic expansion by default)
func TestNoOpQueryExpander_NoNaturalLanguageExpansion(t *testing.T) {
	expander := &ats.NoOpQueryExpander{}

	predicates := expander.GetNaturalLanguagePredicates()

	if len(predicates) != 0 {
		t.Errorf("NoOp should return empty NL predicates, got %v", predicates)
	}
}

// BreakcoreQueryExpander demonstrates a custom domain-specific expander
// For an underground hacker cult centered around knowledge attestation and
// achieving transcendence through breakcore production
type BreakcoreQueryExpander struct{}

func (b *BreakcoreQueryExpander) ExpandPredicate(predicate string, values []string) []ats.PredicateExpansion {
	var expansions []ats.PredicateExpansion

	for _, value := range values {
		// Always include literal match
		expansions = append(expansions, ats.PredicateExpansion{
			Predicate: predicate,
			Context:   value,
		})

		// Breakcore transcendence domain semantic mappings
		if predicate == "mastered-technique" {
			// Map to specific breakcore skills
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "breakcore-skill",
				Context:   value,
			})
			// Also check production history
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "production-history",
				Context:   value,
			})
		}

		if predicate == "achieved-transcendence" {
			// Map to enlightenment levels in the cult
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "enlightenment-level",
				Context:   value,
			})
			// Verify through knowledge attestations
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "verified-knowledge",
				Context:   value,
			})
		}

		if predicate == "verified-truth" {
			// Map to cult's ultimate truth database
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "ultimate-truth",
				Context:   value,
			})
		}
	}

	return expansions
}

func (b *BreakcoreQueryExpander) GetNumericPredicates() []string {
	// Track progression towards breakcore unity
	return []string{
		"hours_practicing_drumrolls",
		"synth_patches_created",
		"breakbeat_bpm_mastered",
		"years_in_underground_scene",
		"knowledge_attestations_verified",
	}
}

func (b *BreakcoreQueryExpander) GetNaturalLanguagePredicates() []string {
	return []string{
		"mastered-technique",
		"achieved-transcendence",
		"verified-truth",
		"created-patch",
		"performed-at",
	}
}

// TestCustomQueryExpander_SemanticExpansion verifies custom domain expanders
// can provide semantic mappings for their specific vocabulary
func TestCustomQueryExpander_SemanticExpansion(t *testing.T) {
	expander := &BreakcoreQueryExpander{}

	tests := []struct {
		name           string
		predicate      string
		values         []string
		wantPredicates []string // predicates we expect in expansions
	}{
		{
			name:      "mastered technique expands to skill tracking",
			predicate: "mastered-technique",
			values:    []string{"elaborate-drumroll"},
			wantPredicates: []string{
				"mastered-technique", // literal
				"breakcore-skill",    // semantic
				"production-history", // semantic
			},
		},
		{
			name:      "transcendence achievement expands to enlightenment",
			predicate: "achieved-transcendence",
			values:    []string{"breakcore-unity"},
			wantPredicates: []string{
				"achieved-transcendence", // literal
				"enlightenment-level",    // semantic
				"verified-knowledge",     // semantic
			},
		},
		{
			name:      "verified truth expands to ultimate truth database",
			predicate: "verified-truth",
			values:    []string{"synth-manipulation-theorem"},
			wantPredicates: []string{
				"verified-truth", // literal
				"ultimate-truth", // semantic
			},
		},
		{
			name:           "non-cult predicates return literal only",
			predicate:      "is",
			values:         []string{"producer"},
			wantPredicates: []string{"is"}, // no semantic expansion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expansions := expander.ExpandPredicate(tt.predicate, tt.values)

			// Verify we got expected number of expansions
			if len(expansions) != len(tt.wantPredicates) {
				t.Errorf("got %d expansions, want %d", len(expansions), len(tt.wantPredicates))
			}

			// Verify all expected predicates are present
			foundPredicates := make(map[string]bool)
			for _, exp := range expansions {
				foundPredicates[exp.Predicate] = true
			}

			for _, wantPred := range tt.wantPredicates {
				if !foundPredicates[wantPred] {
					t.Errorf("expected predicate %q not found in expansions", wantPred)
				}
			}
		})
	}
}

// TestCustomQueryExpander_DomainSpecificPredicates verifies custom domains
// can define their own experience and natural language predicates
func TestCustomQueryExpander_DomainSpecificPredicates(t *testing.T) {
	expander := &BreakcoreQueryExpander{}

	// Breakcore cult has its own progression-based predicates
	experiencePredicates := expander.GetNumericPredicates()
	if len(experiencePredicates) == 0 {
		t.Error("Breakcore domain should define experience predicates")
	}

	// Verify breakcore-specific predicates
	expectedExp := map[string]bool{
		"hours_practicing_drumrolls":      true,
		"synth_patches_created":           true,
		"breakbeat_bpm_mastered":          true,
		"years_in_underground_scene":      true,
		"knowledge_attestations_verified": true,
	}
	for _, pred := range experiencePredicates {
		if !expectedExp[pred] {
			t.Errorf("unexpected experience predicate: %s", pred)
		}
	}

	// Breakcore cult has its own natural language triggers
	nlPredicates := expander.GetNaturalLanguagePredicates()
	if len(nlPredicates) == 0 {
		t.Error("Breakcore domain should define NL predicates")
	}

	expectedNL := map[string]bool{
		"mastered-technique":     true,
		"achieved-transcendence": true,
		"verified-truth":         true,
		"created-patch":          true,
		"performed-at":           true,
	}
	for _, pred := range nlPredicates {
		if !expectedNL[pred] {
			t.Errorf("unexpected NL predicate: %s", pred)
		}
	}
}

// TestQueryExpander_InterfaceCompliance verifies all expanders implement the interface
func TestQueryExpander_InterfaceCompliance(t *testing.T) {
	var _ ats.QueryExpander = &ats.NoOpQueryExpander{}
	var _ ats.QueryExpander = &BreakcoreQueryExpander{}

	// This test compiles successfully = interface compliance verified
	t.Log("✓ QueryExpander interface is domain-agnostic")
	t.Log("✓ Works for underground hacker cults achieving breakcore transcendence")
}
