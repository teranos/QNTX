package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "The axis of time is more useful than a tally": what a namespace is about is
// what it used most recently, each with when, and not what it used most.
func TestCommonTo_MostRecentlyUsedFirst(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	long := time.Date(2026, 9, 1, 10, 15, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		require.NoError(t, store.CreateAttestation(&types.As{
			ID: fmt.Sprintf("often-%d", i), Subjects: []string{"s"}, Predicates: []string{"often"},
			Contexts: []string{"c"}, Actors: []string{"a"}, Timestamp: long, Source: "test",
		}))
	}
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "lately", Subjects: []string{"s"}, Predicates: []string{"lately"},
		Contexts: []string{"c"}, Actors: []string{"a"},
		Timestamp: time.Date(2026, 9, 20, 8, 5, 0, 0, time.UTC), Source: "test",
	}))

	common, err := commonTo(db, "attestation_predicates", "predicate", 1, "")
	require.NoError(t, err)
	require.Len(t, common, 1, "the one shown is the latest, not the most used")
	assert.Equal(t, Common{Name: "lately", Last: "2026-09-20T08:05:00Z", Over: map[string]int64{"2026-09-20T08": 1}}, common[0])

	common, err = commonTo(db, "attestation_predicates", "predicate", commonAtMost, "")
	require.NoError(t, err)
	require.Len(t, common, 2)
	assert.Equal(t, "often", common[1].Name)
	assert.Equal(t, map[string]int64{"2026-09-01T10": 5}, common[1].Over)

	// Every value is cut at the hour the namespace's line starts, so one is
	// never drawn shorter than another for having been busier.
	common, err = commonTo(db, "attestation_predicates", "predicate", commonAtMost, "2026-09-10T00")
	require.NoError(t, err)
	require.Len(t, common, 2)
	assert.Equal(t, "2026-09-01T10:15:00Z", common[1].Last, "when last used is not cut")
	assert.Empty(t, common[1].Over)
}

// The distilled predicates named are the most recently observed, each with when.
func TestQueryDistillStats_MostRecentlyObservedFirst(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	sigma := func(id, predicate, lastSeen string) {
		require.NoError(t, store.CreateAttestation(&types.As{
			ID: id, Subjects: []string{"s"}, Predicates: []string{predicate},
			Contexts: []string{"c"}, Actors: []string{"a"}, Timestamp: time.Now(), Source: "distill",
			Attributes: map[string]any{"_count": 1, "_first_seen": lastSeen, "_last_seen": lastSeen},
		}))
	}
	for i := 0; i < 5; i++ {
		sigma(fmt.Sprintf("often-%d", i), "often", "2026-09-01T10:00:00+00:00")
	}
	sigma("lately", "lately", "2026-09-20T08:05:00+00:00")

	stats, err := queryDistillStats(db)
	require.NoError(t, err)
	predicates, ok := stats["predicates"].([]map[string]any)
	require.True(t, ok, "predicates is %T", stats["predicates"])
	require.Len(t, predicates, 2)
	assert.Equal(t, map[string]any{"predicate": "lately", "last": "2026-09-20T08:05:00Z"}, predicates[0])
	assert.Equal(t, "often", predicates[1]["predicate"])
	assert.NotContains(t, predicates[1], "count")
}

// A namespace is described from its own file. Opening it by its path handed
// back the node's store, so every namespace read the same thing.
func TestDimensionsOf_ReadsTheNamespacesOwnFile(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "one", Subjects: []string{"s"}, Predicates: []string{"page_view"},
		Contexts: []string{"c"}, Actors: []string{"a"},
		Timestamp: time.Date(2026, 9, 20, 8, 5, 0, 0, time.UTC), Source: "test",
	}))

	landing := Landing{Namespace: "Clean", Path: "namespaces/clean.db", DB: db}
	require.NoError(t, bareNode().dimensionsOf(&landing))
	assert.Equal(t, 1, landing.Subjects)
	assert.Equal(t, map[string]int64{"2026-09-20T08": 1}, landing.Over)
	require.Len(t, landing.TopPredicates, 1)
	assert.Equal(t, "page_view", landing.TopPredicates[0].Name)

	err := bareNode().dimensionsOf(&Landing{Namespace: "Clean", Path: "namespaces/clean.db"})
	assert.ErrorContains(t, err, "namespaces/clean.db")
}

// The panel said access_tokens was record only for a week after the table
// landed, because the answer was a list in the crate. The schema answers now.
func TestHeldOnNode_IsTheSchemasAnswer(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	spend := []Spend{
		{Of: "access_tokens", Request: "GET"},
		{Of: "users", Request: "GET"},
		{Of: "schedule_ticks", Request: "GET"},
		{Of: "compaction", Request: "GET"},
	}
	require.NoError(t, heldOnNode(db, spend))

	held := map[string]bool{}
	for _, one := range spend {
		held[one.Of] = one.HeldOnNode
	}
	assert.Equal(t, map[string]bool{
		"access_tokens":  true,
		"users":          true,
		"schedule_ticks": false,
		"compaction":     false,
	}, held)
}
