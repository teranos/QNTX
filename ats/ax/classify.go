package ax

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax/classification"
)

// Classifier defines the interface for conflict classification implementations.
// Both Go (SmartClassifier) and WASM (WasmClassifier) implement this.
type Classifier interface {
	// ClassifyConflicts performs smart classification on claim groups.
	ClassifyConflicts(claimGroups map[string][]ats.IndividualClaim) classification.ClassificationResult

	// GetActorCredibility returns the credibility for a given actor.
	GetActorCredibility(actor string) classification.ActorCredibility

	// GetHighestCredibility returns the highest credibility from a list of actors.
	GetHighestCredibility(actors []string) classification.ActorCredibility
}

// ClassifierBackend indicates which classification implementation is in use
type ClassifierBackend string

const (
	ClassifierBackendGo   ClassifierBackend = "go"
	ClassifierBackendWasm ClassifierBackend = "wasm"
)

// GoClassifier wraps the existing Go SmartClassifier to satisfy the Classifier interface.
// NOTE: The Go classification package is the fallback. Once Rust WASM becomes
// the standard build target, this wrapper and the Go classification package can
// be removed — the Rust engine is the single source of truth for classification.
type GoClassifier struct {
	sc *classification.SmartClassifier
}

// NewGoClassifier creates a Go-backed classifier.
func NewGoClassifier(config classification.TemporalConfig) *GoClassifier {
	return &GoClassifier{sc: classification.NewSmartClassifier(config)}
}

func (g *GoClassifier) ClassifyConflicts(claimGroups map[string][]ats.IndividualClaim) classification.ClassificationResult {
	return g.sc.ClassifyConflicts(claimGroups)
}

func (g *GoClassifier) GetActorCredibility(actor string) classification.ActorCredibility {
	return g.sc.GetActorCredibility(actor)
}

func (g *GoClassifier) GetHighestCredibility(actors []string) classification.ActorCredibility {
	return g.sc.GetHighestCredibility(actors)
}

// Backend returns which implementation is in use.
func (g *GoClassifier) Backend() ClassifierBackend {
	return ClassifierBackendGo
}
