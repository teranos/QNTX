package ax

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax/classification"
)

// Classifier defines the interface for conflict classification implementations.
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
