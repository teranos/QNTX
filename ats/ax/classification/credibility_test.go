package classification

import (
	"testing"
)

// The Attestation Chronicles: Credibility testing in the resistance network.
// This tests how the system evaluates trust levels across different actor types,
// representing the shift from centralized Matrix authority to decentralized
// human verification networks.

func TestCredibilityManager_ClassifyActor(t *testing.T) {
	cm := NewCredibilityManager()

	// Testing actor classification across the resistance network
	tests := []struct {
		actor        string
		expectedType ActorType
		description  string
	}{
		{"morpheus@nebuchadnezzar", ActorTypeHuman, "Resistance leader should be classified as human"},
		{"trinity@zion-command", ActorTypeHuman, "Another resistance member should be human"},
		{"claude-code+claude-sonnet-4-20250514@anthropic", ActorTypeHuman, "Claude assistant is treated as human-guided"},
		{"oracle-ai", ActorTypeLLM, "Oracle AI should be classified as LLM"},
		{"llm-analyst", ActorTypeLLM, "LLM systems should be classified as LLM"},
		{"ai-verification", ActorTypeLLM, "AI verifiers should be classified as LLM"},
		{"ats+ship-systems", ActorTypeSystem, "QNTX ship systems should be classified as system"},
		{"zion-mainframe", ActorTypeLLM, "Haven systems without 'ats+' are classified as LLM"},
		{"nebuchadnezzar-bot", ActorTypeSystem, "Ship bots should be classified as system"},
		{"automated-defense", ActorTypeSystem, "Automated defenses should be classified as system"},
		{"matrix-surveillance", ActorTypeExternal, "Matrix surveillance should be classified as external"},
		{"corporate-hr", ActorTypeExternal, "Corporate systems should be classified as external"},
		{"agent-smith", ActorTypeExternal, "Agent programs should be classified as external"},
		{"unknown-informant", ActorTypeExternal, "Unknown sources should default to external"},
		{"defense-bot@haven", ActorTypeSystem, "Haven defense bots should be system"},
	}

	for _, test := range tests {
		result := cm.GetActorCredibility(test.actor)
		if result.Type != test.expectedType {
			t.Errorf("%s: expected %v, got %v", test.description, test.expectedType, result.Type)
		}
	}
}

func TestCredibilityManager_GetActorCredibility(t *testing.T) {
	cm := NewCredibilityManager()

	// Test authority scores reflect decentralized trust model
	// Humans (resistance) have highest authority, external (Matrix) lowest
	humanCred := cm.GetActorCredibility("niobe@logos")
	if humanCred.Authority != 0.9 {
		t.Errorf("Expected resistance member authority 0.9, got %f", humanCred.Authority)
	}

	llmCred := cm.GetActorCredibility("oracle-assistant")
	if llmCred.Authority != 0.5 {
		t.Errorf("Expected external/LLM authority 0.5, got %f", llmCred.Authority)
	}

	systemCred := cm.GetActorCredibility("ats+ship-defense")
	if systemCred.Authority != 0.4 {
		t.Errorf("Expected ship system authority 0.4, got %f", systemCred.Authority)
	}

	externalCred := cm.GetActorCredibility("matrix-data-mining")
	if externalCred.Authority != 0.5 {
		t.Errorf("Expected Matrix system authority 0.5, got %f", externalCred.Authority)
	}
}

func TestCredibilityManager_InferDomain(t *testing.T) {
	cm := NewCredibilityManager()

	// Domain inference reflects classification by actor patterns
	tests := []struct {
		actor          string
		expectedDomain string
	}{
		{"hr@command-center", "HR"},
		{"human-resources", "HR"},
		{"platform-bot", "Social"},
		{"network-bot", "Social"},
		{"technical-systems", "Technical"},
		{"repository-webhook", "Technical"},
		{"verification-system", "Verification"},
		{"audit-bot", "Verification"},
		{"unknown-contact", "General"},
	}

	for _, test := range tests {
		cred := cm.GetActorCredibility(test.actor)
		if cred.Domain != test.expectedDomain {
			t.Errorf("Actor %s: expected domain %s, got %s", test.actor, test.expectedDomain, cred.Domain)
		}
	}
}

func TestCredibilityManager_SetActorCredibility(t *testing.T) {
	cm := NewCredibilityManager()

	// Set special credibility for The One
	customCred := ActorCredibility{
		Type:      ActorTypeHuman,
		Authority: 0.95,
		Domain:    "Anomaly",
	}

	cm.SetActorCredibility("neo@the-one", customCred)

	result := cm.GetActorCredibility("neo@the-one")
	if result.Authority != 0.95 {
		t.Errorf("Expected The One's authority 0.95, got %f", result.Authority)
	}
	if result.Domain != "Anomaly" {
		t.Errorf("Expected Anomaly domain, got %s", result.Domain)
	}
}

func TestCredibilityManager_GetHighestCredibility(t *testing.T) {
	cm := NewCredibilityManager()

	// Test credibility hierarchy: resistance > AI assistants > Matrix systems
	actors := []string{
		"ship-defense@nebuchadnezzar", // system
		"oracle-assistant",            // LLM
		"morpheus@resistance",         // human
		"matrix-surveillance",         // external
	}

	highest := cm.GetHighestCredibility(actors)
	if highest.Type != ActorTypeHuman {
		t.Errorf("Expected highest credibility to be human, got %v", highest.Type)
	}
	if highest.Authority != 0.9 {
		t.Errorf("Expected highest authority 0.9, got %f", highest.Authority)
	}
}

func TestCredibilityManager_RankActors(t *testing.T) {
	cm := NewCredibilityManager()

	// Test actor ranking in the resistance network
	actors := []string{
		"ats+ship-navigation",  // system (0.4)
		"oracle-llm",           // LLM (0.6)
		"matrix-external-feed", // external (0.5)
		"tank@nebuchadnezzar",  // human (0.9)
	}

	rankings := cm.RankActors(actors)

	if len(rankings) != 4 {
		t.Errorf("Expected 4 rankings, got %d", len(rankings))
	}

	// Should be ranked by authority (highest first)
	expectedOrder := []ActorType{
		ActorTypeHuman,    // 0.9
		ActorTypeLLM,      // 0.6
		ActorTypeExternal, // 0.5
		ActorTypeSystem,   // 0.4
	}

	for i, expected := range expectedOrder {
		if rankings[i].Credibility.Type != expected {
			t.Errorf("Position %d: expected %v, got %v", i, expected, rankings[i].Credibility.Type)
		}
	}
}

func TestCredibilityManager_IsHumanActor(t *testing.T) {
	cm := NewCredibilityManager()

	// Test human actor detection in the resistance
	if !cm.IsHumanActor("zee@haven-engineering") {
		t.Error("Expected zee@haven-engineering to be human")
	}

	if cm.IsHumanActor("oracle-assistant") {
		t.Error("Expected oracle-assistant to not be human")
	}

	if cm.IsHumanActor("ats+ship-systems") {
		t.Error("Expected ats+ship-systems to not be human")
	}
}

func TestCredibilityManager_IsSystemActor(t *testing.T) {
	cm := NewCredibilityManager()

	// Test system actor detection (both ship systems and LLMs)
	if !cm.IsSystemActor("ats+defensive-grid") {
		t.Error("Expected ats+defensive-grid to be system")
	}

	if !cm.IsSystemActor("oracle-llm") {
		t.Error("Expected oracle-llm to be system (LLM is considered system)")
	}

	if cm.IsSystemActor("dozer@haven-engineering") {
		t.Error("Expected dozer@haven-engineering to not be system")
	}

	if cm.IsSystemActor("matrix-surveillance") {
		t.Error("Expected matrix-surveillance to not be system")
	}
}
