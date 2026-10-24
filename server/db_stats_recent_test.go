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
