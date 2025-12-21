package classification

import (
	"time"

	"github.com/sbvh/qntx/ats/types"
)

// SmartClassifier performs advanced conflict classification and resolution
type SmartClassifier struct {
	credibilityManager   *CredibilityManager
	temporalAnalyzer     *TemporalAnalyzer
	confidenceCalculator *ConfidenceCalculator
	config               TemporalConfig
}

// NewSmartClassifier creates a new smart classification engine
func NewSmartClassifier(config TemporalConfig) *SmartClassifier {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(config)
	cc := NewConfidenceCalculator(cm, ta)

	return &SmartClassifier{
		credibilityManager:   cm,
		temporalAnalyzer:     ta,
		confidenceCalculator: cc,
		config:               config,
	}
}

// ClassifyConflicts performs smart classification on a set of claims
func (sc *SmartClassifier) ClassifyConflicts(claimGroups map[string][]IndividualClaim) ClassificationResult {
	var conflicts []AdvancedConflict
	autoResolved := 0
	reviewRequired := 0
	totalAnalyzed := 0

	for claimKey, claims := range claimGroups {
		if len(claims) <= 1 {
			continue // No conflict with single claim
		}

		totalAnalyzed++
		conflict := sc.classifySingleConflict(claimKey, claims)

		if conflict.AutoResolved {
			autoResolved++
		} else if conflict.Type == ResolutionReview {
			reviewRequired++
		}

		conflicts = append(conflicts, conflict)
	}

	return ClassificationResult{
		Conflicts:      conflicts,
		AutoResolved:   autoResolved,
		ReviewRequired: reviewRequired,
		TotalAnalyzed:  totalAnalyzed,
	}
}

// IndividualClaim represents a single claim from cartesian expansion
type IndividualClaim struct {
	Subject   string
	Predicate string
	Context   string
	Actor     string
	Timestamp time.Time
	SourceAs  types.As
}

// classifySingleConflict classifies a single conflict situation
func (sc *SmartClassifier) classifySingleConflict(claimKey string, claims []IndividualClaim) AdvancedConflict {
	// Convert to ClaimWithTiming for analysis
	claimsWithTiming := make([]ClaimWithTiming, len(claims))
	for i, claim := range claims {
		claimsWithTiming[i] = ClaimWithTiming{
			Actor:     claim.Actor,
			Timestamp: claim.Timestamp,
			Predicate: claim.Predicate,
			Subject:   claim.Subject,
			Context:   claim.Context,
		}
	}

	// Calculate confidence
	confidence := sc.confidenceCalculator.CalculateConfidence(claimsWithTiming)

	// Determine resolution type
	resolutionType := sc.determineResolutionType(claims)

	// Determine strategy
	strategy := sc.determineStrategy(resolutionType, confidence)

	// Analyze temporal pattern
	temporalPattern := sc.analyzeTemporalPattern(claims)

	// Create actor hierarchy
	actorHierarchy := sc.createActorHierarchy(claims)

	// Create basic conflict for embedding
	basicConflict := types.Conflict{
		Subject:      claims[0].Subject,
		Predicate:    claims[0].Predicate,
		Context:      claims[0].Context,
		Attestations: sc.getUniqueSourceAttestations(claims),
		Resolution:   string(resolutionType),
	}

	return AdvancedConflict{
		Conflict:        basicConflict,
		Type:            resolutionType,
		Confidence:      confidence,
		Strategy:        strategy,
		ActorHierarchy:  actorHierarchy,
		TemporalPattern: temporalPattern,
		AutoResolved:    resolutionType != ResolutionReview,
	}
}

// determineResolutionType determines the type of resolution needed
func (sc *SmartClassifier) determineResolutionType(claims []IndividualClaim) ResolutionType {
	// Check for same actor evolution
	if sc.isSameActorEvolution(claims) {
		return ResolutionEvolution
	}

	// Check for simultaneous verification
	if sc.isSimultaneousVerification(claims) {
		return ResolutionVerification
	}

	// Check for different contexts (coexistence)
	if sc.isDifferentContexts(claims) {
		return ResolutionCoexistence
	}

	// Check for clear supersession by human operator
	if sc.hasHumanSupersession(claims) {
		return ResolutionSupersession
	}

	// Default to review for unclear cases
	return ResolutionReview
}

// isSameActorEvolution checks if claims represent evolution by same actor
func (sc *SmartClassifier) isSameActorEvolution(claims []IndividualClaim) bool {
	if len(claims) < 2 {
		return false
	}

	// Check if all claims are from same actor
	firstActor := claims[0].Actor
	for _, claim := range claims[1:] {
		if claim.Actor != firstActor {
			return false
		}
	}

	// Check if timestamps suggest evolution rather than duplication
	var timestamps []time.Time
	for _, claim := range claims {
		timestamps = append(timestamps, claim.Timestamp)
	}

	// Sort timestamps
	for i := 0; i < len(timestamps)-1; i++ {
		for j := i + 1; j < len(timestamps); j++ {
			if timestamps[i].After(timestamps[j]) {
				timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
			}
		}
	}

	// Check if there are meaningful gaps between timestamps
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap > sc.config.VerificationWindow {
			return true // Evolution if gaps > verification window
		}
	}

	return false
}

// isSimultaneousVerification checks if claims are simultaneous verification
func (sc *SmartClassifier) isSimultaneousVerification(claims []IndividualClaim) bool {
	if len(claims) < 2 {
		return false
	}

	// Check if all claims have same predicate
	firstPredicate := claims[0].Predicate
	for _, claim := range claims[1:] {
		if claim.Predicate != firstPredicate {
			return false
		}
	}

	// Check if all timestamps are within verification window
	firstTime := claims[0].Timestamp
	for _, claim := range claims[1:] {
		if !sc.temporalAnalyzer.IsSimultaneous(firstTime, claim.Timestamp) {
			return false
		}
	}

	return true
}

// isDifferentContexts checks if claims are about different contexts
func (sc *SmartClassifier) isDifferentContexts(claims []IndividualClaim) bool {
	contexts := make(map[string]bool)
	for _, claim := range claims {
		contexts[claim.Context] = true
	}
	return len(contexts) > 1
}

// hasHumanSupersession checks if human operator overrides other actors
func (sc *SmartClassifier) hasHumanSupersession(claims []IndividualClaim) bool {
	hasHuman := false
	hasNonHuman := false

	for _, claim := range claims {
		if sc.credibilityManager.IsHumanActor(claim.Actor) {
			hasHuman = true
		} else {
			hasNonHuman = true
		}
	}

	return hasHuman && hasNonHuman
}

// determineStrategy determines the resolution strategy
func (sc *SmartClassifier) determineStrategy(resType ResolutionType, confidence float64) string {
	if sc.confidenceCalculator.RequiresHumanReview(confidence) {
		return "human_review"
	}

	switch resType {
	case ResolutionEvolution:
		return "show_latest"
	case ResolutionVerification:
		return "show_all_sources"
	case ResolutionCoexistence:
		return "show_all_contexts"
	case ResolutionSupersession:
		return "show_highest_authority"
	default:
		return "flag_for_review"
	}
}

// analyzeTemporalPattern analyzes temporal patterns in claims
func (sc *SmartClassifier) analyzeTemporalPattern(claims []IndividualClaim) string {
	var timings []ClaimTiming
	for _, claim := range claims {
		timings = append(timings, ClaimTiming{
			Actor:     claim.Actor,
			Timestamp: claim.Timestamp,
			Predicate: claim.Predicate,
		})
	}

	pattern := sc.temporalAnalyzer.AnalyzeTemporalPattern(timings)
	return string(pattern)
}

// createActorHierarchy creates a hierarchy of actors by credibility
func (sc *SmartClassifier) createActorHierarchy(claims []IndividualClaim) []ActorRanking {
	var actors []string
	for _, claim := range claims {
		actors = append(actors, claim.Actor)
	}

	rankings := sc.credibilityManager.RankActors(actors)

	// Add timestamps to rankings
	for i, ranking := range rankings {
		for _, claim := range claims {
			if claim.Actor == ranking.Actor {
				rankings[i].Timestamp = claim.Timestamp
				break
			}
		}
	}

	return rankings
}

// getUniqueSourceAttestations extracts unique source attestations
func (sc *SmartClassifier) getUniqueSourceAttestations(claims []IndividualClaim) []types.As {
	seen := make(map[string]bool)
	var attestations []types.As

	for _, claim := range claims {
		if !seen[claim.SourceAs.ID] {
			seen[claim.SourceAs.ID] = true
			attestations = append(attestations, claim.SourceAs)
		}
	}

	return attestations
}

// SetCustomCredibility allows setting custom credibility for specific actors
func (sc *SmartClassifier) SetCustomCredibility(actor string, credibility ActorCredibility) {
	sc.credibilityManager.SetActorCredibility(actor, credibility)
}

// SetReviewThreshold sets the confidence threshold for human review
func (sc *SmartClassifier) SetReviewThreshold(threshold float64) {
	sc.confidenceCalculator.SetReviewThreshold(threshold)
}

// GetActorCredibility returns the credibility for a given actor
func (sc *SmartClassifier) GetActorCredibility(actor string) ActorCredibility {
	return sc.credibilityManager.GetActorCredibility(actor)
}

// GetHighestCredibility returns the highest credibility actor from a list
func (sc *SmartClassifier) GetHighestCredibility(actors []string) ActorCredibility {
	return sc.credibilityManager.GetHighestCredibility(actors)
}
