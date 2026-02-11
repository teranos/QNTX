//go:build qntxwasm

package ax

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax/classification"
	"github.com/teranos/QNTX/ats/types"
)

// TestClassificationParity runs identical inputs through Go and WASM classifiers
// and compares every output field. If these diverge, one implementation has drifted.
func TestClassificationParity(t *testing.T) {
	config := classification.DefaultTemporalConfig()

	goClassifier := NewGoClassifier(config)

	wasmClassifier, err := NewWasmClassifier(config)
	require.NoError(t, err, "WASM classifier must initialize — is the binary built?")
	require.Equal(t, ClassifierBackendWasm, wasmClassifier.Backend())

	now := time.Now()

	cases := []struct {
		name        string
		claimGroups map[string][]ats.IndividualClaim
	}{
		{
			name: "same actor evolution",
			claimGroups: map[string][]ats.IndividualClaim{
				"ALICE|role|GitHub|human:alice": {
					{Subject: "ALICE", Predicate: "is_junior", Context: "GitHub", Actor: "human:alice", Timestamp: now.Add(-3 * time.Hour), SourceAs: types.As{ID: "as-1"}},
					{Subject: "ALICE", Predicate: "is_senior", Context: "GitHub", Actor: "human:alice", Timestamp: now.Add(-1 * time.Minute), SourceAs: types.As{ID: "as-2"}},
				},
			},
		},
		{
			name: "simultaneous verification",
			claimGroups: map[string][]ats.IndividualClaim{
				"ALICE|is_author|GitHub": {
					{Subject: "ALICE", Predicate: "is_author", Context: "GitHub", Actor: "human:alice", Timestamp: now.Add(-10 * time.Second), SourceAs: types.As{ID: "as-1"}},
					{Subject: "ALICE", Predicate: "is_author", Context: "GitHub", Actor: "system:ci", Timestamp: now.Add(-5 * time.Second), SourceAs: types.As{ID: "as-2"}},
				},
			},
		},
		{
			name: "different contexts coexistence",
			claimGroups: map[string][]ats.IndividualClaim{
				"ALICE|role": {
					{Subject: "ALICE", Predicate: "is_dev", Context: "GitHub", Actor: "human:alice", Timestamp: now.Add(-10 * time.Second), SourceAs: types.As{ID: "as-1"}},
					{Subject: "ALICE", Predicate: "is_maintainer", Context: "GitLab", Actor: "human:bob", Timestamp: now.Add(-5 * time.Second), SourceAs: types.As{ID: "as-2"}},
				},
			},
		},
		{
			name: "human supersedes LLM",
			claimGroups: map[string][]ats.IndividualClaim{
				"ALICE|role|GitHub": {
					{Subject: "ALICE", Predicate: "is_junior", Context: "GitHub", Actor: "llm:gpt-4", Timestamp: now.Add(-10 * time.Second), SourceAs: types.As{ID: "as-1"}},
					{Subject: "ALICE", Predicate: "is_senior", Context: "GitHub", Actor: "human:alice", Timestamp: now.Add(-5 * time.Second), SourceAs: types.As{ID: "as-2"}},
				},
			},
		},
		{
			name: "single claim no conflict",
			claimGroups: map[string][]ats.IndividualClaim{
				"ALICE|is_dev|GitHub": {
					{Subject: "ALICE", Predicate: "is_dev", Context: "GitHub", Actor: "human:alice", Timestamp: now, SourceAs: types.As{ID: "as-1"}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goResult := goClassifier.ClassifyConflicts(tc.claimGroups)
			wasmResult := wasmClassifier.ClassifyConflicts(tc.claimGroups)

			// Counts
			assert.Equal(t, goResult.TotalAnalyzed, wasmResult.TotalAnalyzed, "TotalAnalyzed")
			assert.Equal(t, goResult.AutoResolved, wasmResult.AutoResolved, "AutoResolved")
			assert.Equal(t, goResult.ReviewRequired, wasmResult.ReviewRequired, "ReviewRequired")
			assert.Equal(t, len(goResult.Conflicts), len(wasmResult.Conflicts), "conflict count")

			// Per-conflict fields
			for i := range goResult.Conflicts {
				if i >= len(wasmResult.Conflicts) {
					break
				}
				gc := goResult.Conflicts[i]
				wc := wasmResult.Conflicts[i]

				assert.Equal(t, gc.Type, wc.Type, "conflict[%d] Type", i)
				assert.Equal(t, gc.Strategy, wc.Strategy, "conflict[%d] Strategy", i)
				assert.Equal(t, gc.AutoResolved, wc.AutoResolved, "conflict[%d] AutoResolved", i)
				assert.Equal(t, gc.TemporalPattern, wc.TemporalPattern, "conflict[%d] TemporalPattern", i)
				assert.InDelta(t, gc.Confidence, wc.Confidence, 0.01, "conflict[%d] Confidence", i)

				assert.Equal(t, gc.Conflict.Subject, wc.Conflict.Subject, "conflict[%d] Subject", i)
				assert.Equal(t, gc.Conflict.Predicate, wc.Conflict.Predicate, "conflict[%d] Predicate", i)
				assert.Equal(t, gc.Conflict.Context, wc.Conflict.Context, "conflict[%d] Context", i)
			}
		})
	}
}
