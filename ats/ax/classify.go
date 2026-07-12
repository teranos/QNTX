package ax

import (
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// ActorType represents the type of actor making an attestation
type ActorType string

const (
	ActorTypeHuman    ActorType = "human"
	ActorTypeLLM      ActorType = "llm"
	ActorTypeSystem   ActorType = "system"
	ActorTypeExternal ActorType = "external"
)

// ActorCredibility represents the credibility and authority of an actor
type ActorCredibility struct {
	Type      ActorType `json:"type"`
	Authority float64   `json:"authority"`
	Domain    string    `json:"domain"`
}

// ResolutionType represents the type of conflict resolution applied
type ResolutionType string

const (
	ResolutionEvolution    ResolutionType = "evolution"
	ResolutionVerification ResolutionType = "verification"
	ResolutionCoexistence  ResolutionType = "coexistence"
	ResolutionSupersession ResolutionType = "supersession"
	ResolutionReview       ResolutionType = "review"
)

// TemporalConfig holds configurable time windows for classification
type TemporalConfig struct {
	VerificationWindow time.Duration `json:"verification_window"`
	EvolutionWindow    time.Duration `json:"evolution_window"`
	ObsolescenceWindow time.Duration `json:"obsolescence_window"`
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
	types.Conflict
	Type            ResolutionType `json:"type"`
	Confidence      float64        `json:"confidence"`
	Strategy        string         `json:"strategy"`
	ActorHierarchy  []ActorRanking `json:"actor_hierarchy"`
	TemporalPattern string         `json:"temporal_pattern"`
	AutoResolved    bool           `json:"auto_resolved"`
}

// ClassificationResult represents the result of conflict classification
type ClassificationResult struct {
	Conflicts         []AdvancedConflict `json:"conflicts"`
	AutoResolved      int                `json:"auto_resolved"`
	ReviewRequired    int                `json:"review_required"`
	TotalAnalyzed     int                `json:"total_analyzed"`
	ResolvedSourceIDs []string           `json:"resolved_source_ids"`
}

// Classifier defines the interface for conflict classification implementations.
type Classifier interface {
	// ClassifyConflicts performs smart classification on claim groups.
	ClassifyConflicts(claimGroups map[string][]ats.IndividualClaim) ClassificationResult
}

// ClassifierBackend indicates which classification implementation is in use
type ClassifierBackend string

const (
	ClassifierBackendGo   ClassifierBackend = "go"
	ClassifierBackendWasm ClassifierBackend = "wasm"
)
