package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/types"
)

// The Rust read path and the Go read path must agree. Go drives the same Rust
// code — expand, classify, dedup — one wazero call at a time; Rust runs it
// without leaving the crate. Same database, same filter, same answer.
//
// These tests exist so the Go path can be deleted with evidence rather than
// confidence.

// resolvedPaths returns the two query stores under comparison: one wired to the
// Rust FFI, one not. Both read the same file.
func resolvedPaths(t *testing.T, attestations []*types.As, aliases [][2]string) (rust, golang *SQLQueryStore, resolver *alias.Resolver) {
	t.Helper()

	store, goDB := createTestStore(t)

	for _, as := range attestations {
		require.NoError(t, store.CreateAttestation(as), "insert %s", as.ID)
	}

	aliasStore := NewAliasStore(goDB)
	for _, pair := range aliases {
		require.NoError(t, aliasStore.CreateAlias(context.Background(), pair[0], pair[1], "test"))
	}

	rustStore, ok := store.(interface {
		QueryFilterResolved(types.AxFilter) ([]*types.As, error)
	})
	require.True(t, ok, "test store does not expose the resolved query path")

	rust = NewSQLQueryStore(goDB)
	rq, ok := rustStore.(RawQuerier)
	require.True(t, ok, "test store is not a RawQuerier")
	rust.SetRawQuerier(rq)

	// No raw querier — every step happens in Go.
	golang = NewSQLQueryStore(goDB)

	return rust, golang, alias.NewResolver(aliasStore)
}

func ids(attestations []types.As) []string {
	out := make([]string, len(attestations))
	for i, as := range attestations {
		out[i] = as.ID
	}
	return out
}

// Without this, every comparison below could be Go against Go and still pass.
func TestResolvedQuery_RoutingIsWhatItClaims(t *testing.T) {
	rust, golang, _ := resolvedPaths(t, nil, nil)
	filter := types.AxFilter{Subjects: []string{"ALICE"}}

	_, supported, err := rust.ExecuteAxQueryResolved(context.Background(), filter)
	require.NoError(t, err)
	require.True(t, supported, "the FFI-backed store must take the Rust path")

	_, supported, err = golang.ExecuteAxQueryResolved(context.Background(), filter)
	require.NoError(t, err)
	require.False(t, supported, "the store without a raw querier must fall through to Go")
}

func TestResolvedQuery_SupersededClaimIsDroppedByBothPaths(t *testing.T) {
	// One actor restating the same claim, far enough apart to read as evolution
	// rather than verification. Only the later attestation survives.
	base := time.Now().Add(-time.Hour).UTC()

	attestations := []*types.As{
		{
			ID:         "AS-old",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:alice"},
			Timestamp:  base,
			Source:     "test",
			CreatedAt:  base,
		},
		{
			ID:         "AS-new",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:alice"},
			Timestamp:  base.Add(30 * time.Minute),
			Source:     "test",
			CreatedAt:  base.Add(30 * time.Minute),
		},
	}

	rust, golang, resolver := resolvedPaths(t, attestations, nil)
	filter := types.AxFilter{Subjects: []string{"ALICE"}}

	rustResult, err := ax.NewAxExecutor(rust, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	goResult, err := ax.NewAxExecutor(golang, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	require.Equal(t, []string{"AS-new"}, ids(rustResult.Attestations),
		"the superseded claim does not survive resolution")
	require.Equal(t, ids(goResult.Attestations), ids(rustResult.Attestations),
		"Rust and Go must resolve to the same attestations, in the same order")
}

func TestResolvedQuery_AliasExpansionMatchesAcrossPaths(t *testing.T) {
	// The attestation names one identifier; the query names the other.
	base := time.Now().Add(-time.Hour).UTC()

	attestations := []*types.As{
		{
			ID:         "AS-aliased",
			Subjects:   []string{"alice@example.com"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:bob"},
			Timestamp:  base,
			Source:     "test",
			CreatedAt:  base,
		},
	}

	rust, golang, resolver := resolvedPaths(t, attestations,
		[][2]string{{"ALICE", "alice@example.com"}})

	filter := types.AxFilter{Subjects: []string{"ALICE"}}

	rustResult, err := ax.NewAxExecutor(rust, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	goResult, err := ax.NewAxExecutor(golang, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	require.Equal(t, []string{"AS-aliased"}, ids(rustResult.Attestations),
		"expansion in Rust finds what was written under the other name")
	require.Equal(t, ids(goResult.Attestations), ids(rustResult.Attestations))
}

func TestResolvedQuery_CoexistingClaimsAllSurviveOnBothPaths(t *testing.T) {
	// Different contexts are different claim groups; nothing is resolved away.
	base := time.Now().Add(-time.Hour).UTC()

	attestations := []*types.As{
		{
			ID:         "AS-gh",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:alice"},
			Timestamp:  base,
			Source:     "test",
			CreatedAt:  base,
		},
		{
			ID:         "AS-gl",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitLab"},
			Actors:     []string{"human:alice"},
			Timestamp:  base.Add(time.Minute),
			Source:     "test",
			CreatedAt:  base.Add(time.Minute),
		},
	}

	rust, golang, resolver := resolvedPaths(t, attestations, nil)
	filter := types.AxFilter{Subjects: []string{"ALICE"}}

	rustResult, err := ax.NewAxExecutor(rust, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	goResult, err := ax.NewAxExecutor(golang, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	require.Len(t, rustResult.Attestations, 2)
	require.ElementsMatch(t, []string{"AS-gh", "AS-gl"}, ids(rustResult.Attestations))
	require.Equal(t, ids(goResult.Attestations), ids(rustResult.Attestations))
}

func TestResolvedQuery_EmptyResultOnBothPaths(t *testing.T) {
	rust, golang, resolver := resolvedPaths(t, nil, nil)
	filter := types.AxFilter{Subjects: []string{"NOBODY"}}

	rustResult, err := ax.NewAxExecutor(rust, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	goResult, err := ax.NewAxExecutor(golang, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	require.Empty(t, rustResult.Attestations)
	require.Empty(t, goResult.Attestations)
}

// The shared FFI functions must keep returning unresolved rows. GetAttestations
// feeds the REST API and the watcher engine, which have never seen
// classification and must not start.
func TestRawQueryPathStaysUnresolved(t *testing.T) {
	base := time.Now().Add(-time.Hour).UTC()

	attestations := []*types.As{
		{
			ID:         "AS-old",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:alice"},
			Timestamp:  base,
			Source:     "test",
			CreatedAt:  base,
		},
		{
			ID:         "AS-new",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"is_dev"},
			Contexts:   []string{"GitHub"},
			Actors:     []string{"human:alice"},
			Timestamp:  base.Add(30 * time.Minute),
			Source:     "test",
			CreatedAt:  base.Add(30 * time.Minute),
		},
	}

	store, _ := createTestStore(t)
	for _, as := range attestations {
		require.NoError(t, store.CreateAttestation(as))
	}

	raw, err := store.GetAttestations(ats.AttestationFilter{Subjects: []string{"ALICE"}})
	require.NoError(t, err)
	require.Len(t, raw, 2,
		"GetAttestations returns both claims; resolution belongs to the ax path alone")
}
