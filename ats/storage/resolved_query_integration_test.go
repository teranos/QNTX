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

// The ax read path runs entirely in crates/ats: alias expansion, cartesian
// claim expansion, classification, resolution. These tests drive it through the
// FFI against a real database.
//
// Until 4f9a9e5 the same assertions ran twice — once through Rust, once through
// the Go implementation — and compared the two. That comparison is what
// justified deleting the Go side; the history holds it. What remains is the
// behaviour itself.

// resolvedStore returns a query store wired to the Rust FFI, plus a resolver
// for alias writes (which have no FFI entry point).
func resolvedStore(t *testing.T, attestations []*types.As, aliases [][2]string) (*SQLQueryStore, *alias.Resolver) {
	t.Helper()

	store, goDB := createTestStore(t)

	for _, as := range attestations {
		require.NoError(t, store.CreateAttestation(as), "insert %s", as.ID)
	}

	aliasStore := NewAliasStore(goDB)
	for _, pair := range aliases {
		require.NoError(t, aliasStore.CreateAlias(context.Background(), pair[0], pair[1], "test"))
	}

	rq, ok := store.(RawQuerier)
	require.True(t, ok, "test store is not a RawQuerier")

	queryStore := NewSQLQueryStore(goDB)
	queryStore.SetRawQuerier(rq)

	return queryStore, alias.NewResolver(aliasStore)
}

func ids(attestations []types.As) []string {
	out := make([]string, len(attestations))
	for i, as := range attestations {
		out[i] = as.ID
	}
	return out
}

func ask(t *testing.T, store *SQLQueryStore, resolver *alias.Resolver, filter types.AxFilter) *types.AxResult {
	t.Helper()
	result, err := ax.NewAxExecutor(store, resolver).ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)
	return result
}

// Without this, every test below could be exercising a path that quietly
// isn't there.
func TestResolvedQuery_RoutingIsWhatItClaims(t *testing.T) {
	store, _ := resolvedStore(t, nil, nil)

	_, supported, err := store.ExecuteAxQueryResolved(context.Background(),
		types.AxFilter{Subjects: []string{"ALICE"}})
	require.NoError(t, err)
	require.True(t, supported, "the FFI-backed store must take the Rust path")

	// A store with no raw querier cannot reach it, and says so.
	bare := NewSQLQueryStore(nil)
	_, supported, err = bare.ExecuteAxQueryResolved(context.Background(), types.AxFilter{})
	require.NoError(t, err)
	require.False(t, supported)
}

func TestResolvedQuery_SupersededClaimIsDropped(t *testing.T) {
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

	store, resolver := resolvedStore(t, attestations, nil)
	result := ask(t, store, resolver, types.AxFilter{Subjects: []string{"ALICE"}})

	require.Equal(t, []string{"AS-new"}, ids(result.Attestations),
		"the superseded claim does not survive resolution")
}

func TestResolvedQuery_AliasExpansionFindsTheOtherName(t *testing.T) {
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

	store, resolver := resolvedStore(t, attestations,
		[][2]string{{"ALICE", "alice@example.com"}})

	result := ask(t, store, resolver, types.AxFilter{Subjects: []string{"ALICE"}})

	require.Equal(t, []string{"AS-aliased"}, ids(result.Attestations),
		"expansion in Rust finds what was written under the other name")
}

func TestResolvedQuery_CoexistingClaimsAllSurvive(t *testing.T) {
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

	store, resolver := resolvedStore(t, attestations, nil)
	result := ask(t, store, resolver, types.AxFilter{Subjects: []string{"ALICE"}})

	require.Len(t, result.Attestations, 2)
	require.ElementsMatch(t, []string{"AS-gh", "AS-gl"}, ids(result.Attestations))
}

func TestResolvedQuery_EmptyResult(t *testing.T) {
	store, resolver := resolvedStore(t, nil, nil)
	result := ask(t, store, resolver, types.AxFilter{Subjects: []string{"NOBODY"}})

	require.Empty(t, result.Attestations)
	require.Equal(t, 0, result.Summary.TotalAttestations)
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
