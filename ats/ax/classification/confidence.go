package classification

import (
	"math"
	"time"
)

// ConfidenceCalculator calculates confidence scores for conflict resolution
type ConfidenceCalculator struct {
	credibilityManager *CredibilityManager
	temporalAnalyzer   *TemporalAnalyzer
	reviewThreshold    float64 // Below this threshold = human review required
}

// NewConfidenceCalculator creates a new confidence calculator
func NewConfidenceCalculator(cm *CredibilityManager, ta *TemporalAnalyzer) *ConfidenceCalculator {
	return &ConfidenceCalculator{
		credibilityManager: cm,
		temporalAnalyzer:   ta,
		reviewThreshold:    0.3, // Default: 30% confidence threshold
	}
}

// SetReviewThreshold sets the confidence threshold below which human review is required
func (cc *ConfidenceCalculator) SetReviewThreshold(threshold float64) {
	cc.reviewThreshold = threshold
}

// CalculateConfidence calculates overall confidence score for a set of claims
func (cc *ConfidenceCalculator) CalculateConfidence(claims []ClaimWithTiming) float64 {
	if len(claims) == 0 {
		return 0.0
	}

	if len(claims) == 1 {
		return cc.calculateSingleClaimConfidence(claims[0])
	}

	// Multi-claim confidence calculation
	baseScore := 0.5

	// Independent source bonus (+0.3 max)
	sourceBonus := cc.calculateSourceDiversityBonus(claims)

	// Actor credibility bonus (+0.2 max)
	credibilityBonus := cc.calculateCredibilityBonus(claims)

	// Temporal pattern bonus (+0.2 max)
	temporalBonus := cc.calculateTemporalBonus(claims)

	// Recency bonus (+0.1 max)
	recencyBonus := cc.calculateRecencyBonus(claims)

	// Consistency bonus (+0.1 max)
	consistencyBonus := cc.calculateConsistencyBonus(claims)

	totalScore := baseScore + sourceBonus + credibilityBonus + temporalBonus + recencyBonus + consistencyBonus

	return math.Min(totalScore, 1.0)
}

// ClaimWithTiming represents a claim with timing information for confidence calculation
type ClaimWithTiming struct {
	Actor     string
	Timestamp time.Time
	Predicate string
	Subject   string
	Context   string
}

// calculateSingleClaimConfidence calculates confidence for a single claim
func (cc *ConfidenceCalculator) calculateSingleClaimConfidence(claim ClaimWithTiming) float64 {
	// Single claims get confidence based on actor credibility and recency
	credibility := cc.credibilityManager.GetActorCredibility(claim.Actor)
	recency := cc.temporalAnalyzer.CalculateRecencyScore(claim.Timestamp)

	// Weight credibility more heavily for single claims
	return (credibility.Authority * 0.7) + (recency * 0.3)
}

// calculateSourceDiversityBonus calculates bonus for having multiple independent sources
func (cc *ConfidenceCalculator) calculateSourceDiversityBonus(claims []ClaimWithTiming) float64 {
	uniqueActors := make(map[string]bool)
	for _, claim := range claims {
		uniqueActors[claim.Actor] = true
	}

	independentCount := len(uniqueActors)
	if independentCount <= 1 {
		return 0.0
	}

	// Bonus increases with more independent sources, max 0.3
	bonus := float64(independentCount-1) * 0.1
	return math.Min(bonus, 0.3)
}

// calculateCredibilityBonus calculates bonus based on highest credibility actor
func (cc *ConfidenceCalculator) calculateCredibilityBonus(claims []ClaimWithTiming) float64 {
	var actors []string
	for _, claim := range claims {
		actors = append(actors, claim.Actor)
	}

	highest := cc.credibilityManager.GetHighestCredibility(actors)
	return highest.Authority * 0.2 // Max 0.2 bonus for highest credibility actor
}

// calculateTemporalBonus calculates bonus based on temporal patterns
func (cc *ConfidenceCalculator) calculateTemporalBonus(claims []ClaimWithTiming) float64 {
	var timings []ClaimTiming
	for _, claim := range claims {
		timings = append(timings, ClaimTiming{
			Actor:     claim.Actor,
			Timestamp: claim.Timestamp,
			Predicate: claim.Predicate,
		})
	}

	temporalConfidence := cc.temporalAnalyzer.CalculateTemporalConfidence(timings)
	return (temporalConfidence - 0.5) * 0.4 // Convert 0.5-1.0 range to 0.0-0.2 bonus
}

// calculateRecencyBonus calculates bonus based on most recent claim
func (cc *ConfidenceCalculator) calculateRecencyBonus(claims []ClaimWithTiming) float64 {
	var mostRecent time.Time
	for _, claim := range claims {
		if claim.Timestamp.After(mostRecent) {
			mostRecent = claim.Timestamp
		}
	}

	recency := cc.temporalAnalyzer.CalculateRecencyScore(mostRecent)
	return recency * 0.1 // Max 0.1 bonus for recency
}

// calculateConsistencyBonus calculates bonus for consistent predicates
func (cc *ConfidenceCalculator) calculateConsistencyBonus(claims []ClaimWithTiming) float64 {
	uniquePredicates := make(map[string]bool)
	for _, claim := range claims {
		uniquePredicates[claim.Predicate] = true
	}

	// If all claims have same predicate, give consistency bonus
	if len(uniquePredicates) == 1 {
		return 0.1
	}

	// If predicates are different but related (e.g., junior/senior developer), smaller bonus
	if cc.areRelatedPredicates(uniquePredicates) {
		return 0.05
	}

	return 0.0
}

// areRelatedPredicates checks if predicates suggest evolution rather than conflict
func (cc *ConfidenceCalculator) areRelatedPredicates(predicates map[string]bool) bool {
	// Simple heuristic: if one predicate contains another, they might be related
	predicateList := make([]string, 0, len(predicates))
	for p := range predicates {
		predicateList = append(predicateList, p)
	}

	for i, p1 := range predicateList {
		for j, p2 := range predicateList {
			if i != j && (contains(p1, p2) || contains(p2, p1)) {
				return true
			}
		}
	}

	return false
}

// contains checks if s1 contains s2 (case-insensitive substring)
func contains(s1, s2 string) bool {
	// Simple substring check - could be enhanced with more sophisticated logic
	return len(s2) > 0 && len(s1) > len(s2) &&
		(s1[:len(s2)] == s2 || s1[len(s1)-len(s2):] == s2)
}

// RequiresHumanReview returns true if confidence is below review threshold
func (cc *ConfidenceCalculator) RequiresHumanReview(confidence float64) bool {
	return confidence < cc.reviewThreshold
}

// GetConfidenceLevel returns a human-readable confidence level
func (cc *ConfidenceCalculator) GetConfidenceLevel(confidence float64) string {
	switch {
	case confidence >= 0.8:
		return "high"
	case confidence >= 0.6:
		return "medium"
	case confidence >= 0.4:
		return "low"
	default:
		return "very_low"
	}
}

// CalculateActorAgreement calculates how much actors agree on the same claim
func (cc *ConfidenceCalculator) CalculateActorAgreement(claims []ClaimWithTiming) float64 {
	if len(claims) <= 1 {
		return 1.0
	}

	// Group claims by predicate
	predicateGroups := make(map[string][]ClaimWithTiming)
	for _, claim := range claims {
		predicateGroups[claim.Predicate] = append(predicateGroups[claim.Predicate], claim)
	}

	// Find the predicate with most agreement
	var maxAgreement int
	for _, group := range predicateGroups {
		if len(group) > maxAgreement {
			maxAgreement = len(group)
		}
	}

	return float64(maxAgreement) / float64(len(claims))
}
