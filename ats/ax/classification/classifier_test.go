package classification

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// The Attestation Chronicles: Testing smart classification in the transition
// from centralized Matrix control to decentralized resistance networks.
// Each test scenario reflects different aspects of identity verification
// as power shifts from large institutions to personal, human-scale attestations.

func TestSmartClassifier_EvolutionDetection(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// NEO's progression from awakening to The One - classic evolution pattern
	claims := []IndividualClaim{
		{
			Subject:   "NEO",
			Predicate: "programmer",
			Context:   "MATRIX",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: time.Now().Add(-2 * time.Hour),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "NEO",
			Predicate: "the_one",
			Context:   "MATRIX",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	if result.Type != ResolutionEvolution {
		t.Errorf("Expected ResolutionEvolution, got %v", result.Type)
	}

	if result.Strategy != "show_latest" {
		t.Errorf("Expected show_latest strategy, got %s", result.Strategy)
	}

	if !result.AutoResolved {
		t.Error("Expected evolution to be auto-resolved")
	}
}

func TestSmartClassifier_SimultaneousVerification(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	now := time.Now()
	// TRINITY's pilot skills verified by multiple independent sources
	claims := []IndividualClaim{
		{
			Subject:   "TRINITY",
			Predicate: "pilot",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: now,
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "TRINITY",
			Predicate: "pilot",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "zion-command@resistance-network",
			Timestamp: now.Add(10 * time.Second), // Within verification window
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	if result.Type != ResolutionVerification {
		t.Errorf("Expected ResolutionVerification, got %v", result.Type)
	}

	if result.Strategy != "show_all_sources" {
		t.Errorf("Expected show_all_sources strategy, got %s", result.Strategy)
	}

	if !result.AutoResolved {
		t.Error("Expected verification to be auto-resolved")
	}
}

func TestSmartClassifier_HumanSupersession(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// Morpheus (human) overrides automated Matrix classification
	claims := []IndividualClaim{
		{
			Subject:   "TANK",
			Predicate: "operator",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "ats+ship-systems",
			Timestamp: time.Now().Add(-1 * time.Hour),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "TANK",
			Predicate: "weapons_specialist",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	if result.Type != ResolutionSupersession {
		t.Errorf("Expected ResolutionSupersession, got %v", result.Type)
	}

	if result.Strategy != "show_highest_authority" {
		t.Errorf("Expected show_highest_authority strategy, got %s", result.Strategy)
	}

	if !result.AutoResolved {
		t.Error("Expected supersession to be auto-resolved")
	}
}

func TestSmartClassifier_DifferentContexts(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// NIOBE holds different roles in different contexts - both valid
	claims := []IndividualClaim{
		{
			Subject:   "NIOBE",
			Predicate: "captain",
			Context:   "LOGOS",
			Actor:     "zion-command@resistance-network",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "NIOBE",
			Predicate: "council_member",
			Context:   "HAVEN",
			Actor:     "zion-command@resistance-network",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	if result.Type != ResolutionCoexistence {
		t.Errorf("Expected ResolutionCoexistence, got %v", result.Type)
	}

	if result.Strategy != "show_all_contexts" {
		t.Errorf("Expected show_all_contexts strategy, got %s", result.Strategy)
	}

	if !result.AutoResolved {
		t.Error("Expected coexistence to be auto-resolved")
	}
}

func TestSmartClassifier_RequiresReview(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// CYPHER's loyalty: demonstrates why decentralized verification matters
	// - untrusted sources create ambiguity that requires human judgment
	claims := []IndividualClaim{
		{
			Subject:   "CYPHER",
			Predicate: "resistance_member",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "unknown-informant",
			Timestamp: time.Now().Add(-30 * time.Minute),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "CYPHER",
			Predicate: "agent_collaborator",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "matrix-surveillance",
			Timestamp: time.Now().Add(-25 * time.Minute),
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	// Should require review due to low confidence from external sources
	if result.AutoResolved {
		t.Error("Expected ambiguous claims to require human review, but got auto-resolved")
	}

	if result.Strategy != "human_review" && result.Strategy != "flag_for_review" {
		t.Errorf("Expected human_review or flag_for_review strategy, got %s", result.Strategy)
	}
}

func TestSmartClassifier_ConfidenceScoring(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// High confidence: LINK's skills verified by multiple trusted sources
	claims := []IndividualClaim{
		{
			Subject:   "LINK",
			Predicate: "operator",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "LINK",
			Predicate: "operator",
			Context:   "NEBUCHADNEZZAR",
			Actor:     "tank@nebuchadnezzar",
			Timestamp: time.Now().Add(5 * time.Second),
			SourceAs:  types.As{ID: "as2"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	if result.Confidence < 0.7 {
		t.Errorf("Expected high confidence (>0.7) for multiple human sources, got %f", result.Confidence)
	}

	// Low confidence: old data from unverified Matrix surveillance
	lowConfidenceClaims := []IndividualClaim{
		{
			Subject:   "DOZER",
			Predicate: "engineer",
			Context:   "HAVEN",
			Actor:     "matrix-data-mining",
			Timestamp: time.Now().Add(-48 * time.Hour),
			SourceAs:  types.As{ID: "as3"},
		},
	}

	lowResult := classifier.classifySingleConflict("test-key", lowConfidenceClaims)

	if lowResult.Confidence > 0.6 {
		t.Errorf("Expected low confidence (<0.6) for old external source, got %f", lowResult.Confidence)
	}
}

func TestSmartClassifier_ActorHierarchy(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// ZEE's profession assessment from different actor types
	claims := []IndividualClaim{
		{
			Subject:   "ZEE",
			Predicate: "engineer",
			Context:   "HAVEN",
			Actor:     "ats+ship-maintenance",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as1"},
		},
		{
			Subject:   "ZEE",
			Predicate: "systems_specialist",
			Context:   "HAVEN",
			Actor:     "claude-code+claude-sonnet-4-20250514@anthropic",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as2"},
		},
		{
			Subject:   "ZEE",
			Predicate: "dock_supervisor",
			Context:   "HAVEN",
			Actor:     "morpheus@nebuchadnezzar",
			Timestamp: time.Now(),
			SourceAs:  types.As{ID: "as3"},
		},
	}

	result := classifier.classifySingleConflict("test-key", claims)

	// The hierarchy reflects the shift from centralized to decentralized attestations
	// Humans (decentralized) > LLMs (semi-autonomous) > Systems (centralized)
	if len(result.ActorHierarchy) != 3 {
		t.Errorf("Expected 3 actors in hierarchy, got %d", len(result.ActorHierarchy))
	}

	// Find human actor in hierarchy (Morpheus should be highest credibility)
	humanFound := false
	for _, actor := range result.ActorHierarchy {
		if actor.Credibility.Type == ActorTypeHuman {
			humanFound = true
			break
		}
	}
	if !humanFound {
		t.Error("Expected to find human actor (morpheus@nebuchadnezzar) in hierarchy")
	}

	// Verify we have the expected actor types represented
	types := make(map[string]bool)
	for i, actor := range result.ActorHierarchy {
		types[string(actor.Credibility.Type)] = true
		t.Logf("Actor %d: %s (type: %s)", i, actor.Actor, actor.Credibility.Type)
	}

	// At minimum, we should have human and either LLM or system
	if !types["human"] {
		t.Error("Expected human actor type to be present")
	}

	// Should have at least 2 different types
	if len(types) < 2 {
		t.Errorf("Expected at least 2 different actor types, got %d: %v", len(types), types)
	}
}

func TestSmartClassifier_ClassifyConflicts(t *testing.T) {
	config := DefaultTemporalConfig()
	classifier := NewSmartClassifier(config)

	// Multiple conflict scenarios from The Attestation Chronicles
	claimGroups := map[string][]IndividualClaim{
		"NEO|profession|MATRIX": {
			{
				Subject:   "NEO",
				Predicate: "programmer",
				Context:   "MATRIX",
				Actor:     "agent-smith@matrix-system",
				Timestamp: time.Now().Add(-2 * time.Hour),
				SourceAs:  types.As{ID: "as1"},
			},
			{
				Subject:   "NEO",
				Predicate: "the_one",
				Context:   "MATRIX",
				Actor:     "morpheus@nebuchadnezzar",
				Timestamp: time.Now(),
				SourceAs:  types.As{ID: "as2"},
			},
		},
		"APOC|status|NEBUCHADNEZZAR": {
			{
				Subject:   "APOC",
				Predicate: "crew_member",
				Context:   "NEBUCHADNEZZAR",
				Actor:     "morpheus@nebuchadnezzar",
				Timestamp: time.Now(),
				SourceAs:  types.As{ID: "as3"},
			},
			{
				Subject:   "APOC",
				Predicate: "crew_member",
				Context:   "NEBUCHADNEZZAR",
				Actor:     "zion-command@resistance-network",
				Timestamp: time.Now().Add(30 * time.Second),
				SourceAs:  types.As{ID: "as4"},
			},
		},
	}

	result := classifier.ClassifyConflicts(claimGroups)

	if len(result.Conflicts) != 2 {
		t.Errorf("Expected 2 conflicts, got %d", len(result.Conflicts))
	}

	if result.TotalAnalyzed != 2 {
		t.Errorf("Expected 2 analyzed, got %d", result.TotalAnalyzed)
	}

	if result.AutoResolved < 1 {
		t.Errorf("Expected at least 1 auto-resolved conflict, got %d", result.AutoResolved)
	}
}
