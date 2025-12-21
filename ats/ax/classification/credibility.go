package classification

import (
	"strings"
)

// CredibilityManager manages actor credibility scoring and classification
type CredibilityManager struct {
	credibilityMap map[string]ActorCredibility
	defaultScores  map[ActorType]float64
}

// NewCredibilityManager creates a new credibility manager with default scores
func NewCredibilityManager() *CredibilityManager {
	defaultScores := map[ActorType]float64{
		ActorTypeHuman:    0.9, // Human operators have highest credibility
		ActorTypeLLM:      0.6, // LLM has medium credibility
		ActorTypeSystem:   0.4, // Automated systems have lower credibility
		ActorTypeExternal: 0.5, // External sources have variable credibility
	}

	return &CredibilityManager{
		credibilityMap: make(map[string]ActorCredibility),
		defaultScores:  defaultScores,
	}
}

// GetActorCredibility returns the credibility for a given actor
func (cm *CredibilityManager) GetActorCredibility(actor string) ActorCredibility {
	// Check if we have specific credibility mapping
	if cred, exists := cm.credibilityMap[actor]; exists {
		return cred
	}

	// Classify actor type based on patterns and return default credibility
	actorType := cm.classifyActor(actor)
	return ActorCredibility{
		Type:      actorType,
		Authority: cm.defaultScores[actorType],
		Domain:    cm.inferDomain(actor),
	}
}

// classifyActor determines the actor type based on naming patterns
func (cm *CredibilityManager) classifyActor(actor string) ActorType {
	actor = strings.ToLower(actor)

	// Human operators - typically email addresses or names
	if strings.Contains(actor, "@") && !strings.Contains(actor, "bot") && !strings.Contains(actor, "system") {
		return ActorTypeHuman
	}

	// LLM/AI systems
	if strings.Contains(actor, "claude") || strings.Contains(actor, "gpt") ||
		strings.Contains(actor, "llm") || strings.Contains(actor, "ai") {
		return ActorTypeLLM
	}

	// External sources (check before system to avoid conflicts like "hr-system")
	// External platforms - detected by common service patterns
	if strings.Contains(actor, "platform") || strings.Contains(actor, "service") ||
		strings.Contains(actor, "registry") || strings.Contains(actor, "webhook") {
		return ActorTypeExternal
	}

	// System actors
	if strings.Contains(actor, "system") || strings.Contains(actor, "ats+") ||
		strings.Contains(actor, "bot") || strings.Contains(actor, "verification") ||
		strings.Contains(actor, "automated") {
		return ActorTypeSystem
	}

	// Default to external for unknown patterns
	return ActorTypeExternal
}

// inferDomain attempts to infer the domain of expertise for an actor
func (cm *CredibilityManager) inferDomain(actor string) string {
	actor = strings.ToLower(actor)

	if strings.Contains(actor, "hr") || strings.Contains(actor, "human") {
		return "HR"
	}
	if strings.Contains(actor, "platform") || strings.Contains(actor, "social") || strings.Contains(actor, "network") {
		return "Social"
	}
	if strings.Contains(actor, "repository") || strings.Contains(actor, "technical") || strings.Contains(actor, "scm") {
		return "Technical"
	}
	if strings.Contains(actor, "verification") || strings.Contains(actor, "audit") {
		return "Verification"
	}

	return "General"
}

// SetActorCredibility allows manual override of actor credibility
func (cm *CredibilityManager) SetActorCredibility(actor string, credibility ActorCredibility) {
	cm.credibilityMap[actor] = credibility
}

// GetHighestCredibility returns the highest credibility actor from a list
func (cm *CredibilityManager) GetHighestCredibility(actors []string) ActorCredibility {
	var highest ActorCredibility
	highestScore := -1.0

	for _, actor := range actors {
		cred := cm.GetActorCredibility(actor)
		if cred.Authority > highestScore {
			highest = cred
			highestScore = cred.Authority
		}
	}

	return highest
}

// RankActors returns actors ranked by credibility (highest first)
func (cm *CredibilityManager) RankActors(actors []string) []ActorRanking {
	rankings := make([]ActorRanking, len(actors))

	for i, actor := range actors {
		rankings[i] = ActorRanking{
			Actor:       actor,
			Credibility: cm.GetActorCredibility(actor),
		}
	}

	// Sort by credibility (highest first)
	for i := 0; i < len(rankings)-1; i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[i].Credibility.Authority < rankings[j].Credibility.Authority {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}

	return rankings
}

// IsHumanActor returns true if the actor is classified as human
func (cm *CredibilityManager) IsHumanActor(actor string) bool {
	return cm.GetActorCredibility(actor).Type == ActorTypeHuman
}

// IsSystemActor returns true if the actor is classified as system/automated
func (cm *CredibilityManager) IsSystemActor(actor string) bool {
	cred := cm.GetActorCredibility(actor)
	return cred.Type == ActorTypeSystem || cred.Type == ActorTypeLLM
}
