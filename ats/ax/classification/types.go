package classification

import (
	"time"

	"github.com/sbvh/qntx/ats/types"
)

// ActorType represents the type of actor making an attestation
type ActorType string

const (
	ActorTypeHuman    ActorType = "human"    // Highest credibility (0.9)
	ActorTypeLLM      ActorType = "llm"      // Medium credibility (0.6)
	ActorTypeSystem   ActorType = "system"   // Lower credibility (0.4)
	ActorTypeExternal ActorType = "external" // Variable credibility (0.3-0.7)
)

// ActorCredibility represents the credibility and authority of an actor
type ActorCredibility struct {
	Type      ActorType `json:"type"`
	Authority float64   `json:"authority"` // 0.0-1.0 credibility score
	Domain    string    `json:"domain"`    // HR, Technical, Social, etc.
}

// ResolutionType represents the type of conflict resolution applied
type ResolutionType string

const (
	ResolutionEvolution    ResolutionType = "evolution"    // Natural progression (same actor, different times)
	ResolutionVerification ResolutionType = "verification" // Independent confirmation (different actors, same time)
	ResolutionCoexistence  ResolutionType = "coexistence"  // Multiple valid assignments (different contexts)
	ResolutionSupersession ResolutionType = "supersession" // Higher authority overrides lower
	ResolutionReview       ResolutionType = "review"       // Low confidence = human review
)

// TemporalConfig holds configurable time windows for classification
type TemporalConfig struct {
	VerificationWindow time.Duration `json:"verification_window"` // Default: 1 minute
	EvolutionWindow    time.Duration `json:"evolution_window"`    // Default: 24 hours
	ObsolescenceWindow time.Duration `json:"obsolescence_window"` // Default: 1 year
}

// DefaultTemporalConfig returns sensible defaults for temporal configuration
func DefaultTemporalConfig() TemporalConfig {
	return TemporalConfig{
		VerificationWindow: 1 * time.Minute,
		EvolutionWindow:    24 * time.Hour,
		ObsolescenceWindow: 365 * 24 * time.Hour,
	}
}

// ActorRanking represents an actor's ranking in conflict resolution
type ActorRanking struct {
	Actor       string           `json:"actor"`
	Credibility ActorCredibility `json:"credibility"`
	Timestamp   time.Time        `json:"timestamp"`
}

// AdvancedConflict extends the basic conflict with smart classification
type AdvancedConflict struct {
	types.Conflict                 // Embed basic conflict
	Type            ResolutionType `json:"type"`
	Confidence      float64        `json:"confidence"`
	Strategy        string         `json:"strategy"`
	ActorHierarchy  []ActorRanking `json:"actor_hierarchy"`
	TemporalPattern string         `json:"temporal_pattern"`
	AutoResolved    bool           `json:"auto_resolved"`
}

// ClassificationResult represents the result of conflict classification
type ClassificationResult struct {
	Conflicts      []AdvancedConflict `json:"conflicts"`
	AutoResolved   int                `json:"auto_resolved"`
	ReviewRequired int                `json:"review_required"`
	TotalAnalyzed  int                `json:"total_analyzed"`
}
