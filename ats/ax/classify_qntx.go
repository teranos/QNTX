//go:build qntxwasm

package ax

import (
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax/classification"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
)

// NewDefaultClassifier creates the WASM-backed classifier.
// Panics if the WASM engine is unavailable — run `make rust-wasm` to build.
//
// TODO(QNTX): Remove ats/ax/classification/ (Go classifier) in a follow-up PR.
func NewDefaultClassifier(config classification.TemporalConfig) Classifier {
	c, err := NewWasmClassifier(config)
	if err != nil {
		panic("WASM classifier unavailable: " + err.Error() + " — run `make rust-wasm`")
	}
	return c
}

// WasmClassifier delegates classification to the Rust engine via WASM (wazero).
// Credibility methods stay in Go since they're used for post-classification
// resolution application and are just pattern matching.
type WasmClassifier struct {
	goFallback *GoClassifier // credibility methods + config access (no classification fallback)
}

// NewWasmClassifier creates a WASM-backed classifier.
// Returns an error if the WASM engine cannot be initialized.
func NewWasmClassifier(config classification.TemporalConfig) (*WasmClassifier, error) {
	if _, err := wasm.GetEngine(); err != nil {
		return nil, err
	}
	return &WasmClassifier{
		goFallback: NewGoClassifier(config),
	}, nil
}

// ClassifyConflicts delegates to the Rust WASM engine for classification.
func (w *WasmClassifier) ClassifyConflicts(claimGroups map[string][]ats.IndividualClaim) classification.ClassificationResult {
	engine, err := wasm.GetEngine()
	if err != nil {
		// WASM engine was available at construction time but is gone now.
		// This should not happen — crash loud so we notice.
		panic("WASM engine unavailable after successful init: " + err.Error())
	}

	input := w.buildWasmInput(claimGroups)

	output, err := engine.ClassifyClaims(input)
	if err != nil {
		panic("WASM classify_claims failed: " + err.Error())
	}

	return w.convertOutput(output, claimGroups)
}

// GetActorCredibility delegates to the Go credibility manager.
func (w *WasmClassifier) GetActorCredibility(actor string) classification.ActorCredibility {
	return w.goFallback.GetActorCredibility(actor)
}

// GetHighestCredibility delegates to the Go credibility manager.
func (w *WasmClassifier) GetHighestCredibility(actors []string) classification.ActorCredibility {
	return w.goFallback.GetHighestCredibility(actors)
}

// Backend returns the classifier backend type.
func (w *WasmClassifier) Backend() ClassifierBackend {
	return ClassifierBackendWasm
}

// buildWasmInput converts Go claim groups to the WASM classify_claims input format.
func (w *WasmClassifier) buildWasmInput(claimGroups map[string][]ats.IndividualClaim) wasm.ClassifyInput {
	groups := make([]wasm.ClassifyClaimGroup, 0, len(claimGroups))

	for key, claims := range claimGroups {
		wasmClaims := make([]wasm.ClassifyClaimInput, len(claims))
		for i, c := range claims {
			wasmClaims[i] = wasm.ClassifyClaimInput{
				Subject:     c.Subject,
				Predicate:   c.Predicate,
				Context:     c.Context,
				Actor:       c.Actor,
				TimestampMs: c.Timestamp.UnixMilli(),
				SourceID:    c.SourceAs.ID,
			}
		}
		groups = append(groups, wasm.ClassifyClaimGroup{
			Key:    key,
			Claims: wasmClaims,
		})
	}

	return wasm.ClassifyInput{
		ClaimGroups: groups,
		Config: wasm.ClassifyTemporalConfig{
			VerificationWindowMs: w.goFallback.sc.Config().VerificationWindow.Milliseconds(),
			EvolutionWindowMs:    w.goFallback.sc.Config().EvolutionWindow.Milliseconds(),
			ObsolescenceWindowMs: w.goFallback.sc.Config().ObsolescenceWindow.Milliseconds(),
		},
		NowMs: time.Now().UnixMilli(),
	}
}

// convertOutput converts WASM ClassifyOutput back to Go ClassificationResult.
func (w *WasmClassifier) convertOutput(output *wasm.ClassifyOutput, claimGroups map[string][]ats.IndividualClaim) classification.ClassificationResult {
	conflicts := make([]classification.AdvancedConflict, len(output.Conflicts))

	for i, wc := range output.Conflicts {
		// Collect unique source attestations from matching claims
		seen := make(map[string]bool)
		var sourceAs []types.As
		for _, claims := range claimGroups {
			for _, claim := range claims {
				if claim.Subject == wc.Subject && claim.Predicate == wc.Predicate && claim.Context == wc.Context {
					if !seen[claim.SourceAs.ID] {
						seen[claim.SourceAs.ID] = true
						sourceAs = append(sourceAs, claim.SourceAs)
					}
				}
			}
		}

		// Rust returns PascalCase conflict types ("Evolution"), Go uses lowercase ("evolution")
		resolutionType := classification.ResolutionType(strings.ToLower(wc.ConflictType))

		conflicts[i] = classification.AdvancedConflict{
			Conflict: types.Conflict{
				Subject:      wc.Subject,
				Predicate:    wc.Predicate,
				Context:      wc.Context,
				Attestations: sourceAs,
				Resolution:   string(resolutionType),
			},
			Type:            resolutionType,
			Confidence:      wc.Confidence,
			Strategy:        wc.Strategy,
			ActorHierarchy:  convertActorHierarchy(wc.ActorHierarchy),
			TemporalPattern: wc.TemporalPattern,
			AutoResolved:    wc.AutoResolved,
		}
	}

	return classification.ClassificationResult{
		Conflicts:      conflicts,
		AutoResolved:   output.AutoResolved,
		ReviewRequired: output.ReviewRequired,
		TotalAnalyzed:  output.TotalAnalyzed,
	}
}

// convertActorHierarchy converts WASM actor rankings to Go ActorRanking types.
func convertActorHierarchy(wasmRankings []wasm.ClassifyActorRank) []classification.ActorRanking {
	rankings := make([]classification.ActorRanking, len(wasmRankings))
	for i, wr := range wasmRankings {
		rankings[i] = classification.ActorRanking{
			Actor: wr.Actor,
			Credibility: classification.ActorCredibility{
				Type:      credibilityStringToType(wr.Credibility),
				Authority: credibilityStringToAuthority(wr.Credibility),
			},
		}
		if wr.Timestamp != nil {
			rankings[i].Timestamp = time.UnixMilli(*wr.Timestamp)
		}
	}
	return rankings
}

func credibilityStringToType(s string) classification.ActorType {
	switch s {
	case "Human":
		return classification.ActorTypeHuman
	case "Llm":
		return classification.ActorTypeLLM
	case "System":
		return classification.ActorTypeSystem
	default:
		return classification.ActorTypeExternal
	}
}

func credibilityStringToAuthority(s string) float64 {
	switch s {
	case "Human":
		return 0.9
	case "Llm":
		return 0.6
	case "System":
		return 0.4
	default:
		return 0.3
	}
}
