package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestExtendedAttestationFiltering(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := NewSQLStore(db, nil)

	// Create test attestations with varied attributes
	now := time.Now()
	testData := []*types.As{
		{
			ID:         "test1",
			Subjects:   []string{"user:alice", "resource:doc1"},
			Predicates: []string{"can:read", "can:write"},
			Contexts:   []string{"project:alpha", "env:dev"},
			Actors:     []string{"service:api", "component:auth"},
			Timestamp:  now,
			Source:     "test",
			CreatedAt:  now,
		},
		{
			ID:         "test2",
			Subjects:   []string{"user:bob", "resource:doc2"},
			Predicates: []string{"can:read", "can:delete"},
			Contexts:   []string{"project:beta", "env:prod"},
			Actors:     []string{"service:web", "component:auth"},
			Timestamp:  now.Add(-1 * time.Hour),
			Source:     "test",
			CreatedAt:  now,
		},
		{
			ID:         "test3",
			Subjects:   []string{"user:alice", "resource:doc3"},
			Predicates: []string{"can:admin", "can:write"},
			Contexts:   []string{"project:alpha", "env:staging"},
			Actors:     []string{"service:api", "component:admin"},
			Timestamp:  now.Add(-2 * time.Hour),
			Source:     "test",
			CreatedAt:  now,
		},
		{
			ID:         "test4",
			Subjects:   []string{"user:charlie", "resource:doc1"},
			Predicates: []string{"can:read"},
			Contexts:   []string{"project:gamma", "env:dev"},
			Actors:     []string{"service:batch", "component:worker"},
			Timestamp:  now.Add(-3 * time.Hour),
			Source:     "test",
			CreatedAt:  now,
		},
	}

	// Insert test data
	for _, as := range testData {
		err := store.CreateAttestation(as)
		require.NoError(t, err)
	}

	t.Run("filter by subjects", func(t *testing.T) {
		// Test single subject filter
		results, err := store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{"user:alice"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Test multiple subjects (OR logic)
		results, err = store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{"user:alice", "user:bob"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 3) // test1, test2, and test3

		// Test resource subject
		results, err = store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{"resource:doc1"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test4
	})

	t.Run("filter by predicates", func(t *testing.T) {
		// Test single predicate
		results, err := store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{"can:write"},
			Limit:      10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Test multiple predicates (OR logic)
		results, err = store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{"can:delete", "can:admin"},
			Limit:      10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test2 and test3
	})

	t.Run("filter by contexts", func(t *testing.T) {
		// Test single context
		results, err := store.GetAttestations(ats.AttestationFilter{
			Contexts: []string{"project:alpha"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Test environment context
		results, err = store.GetAttestations(ats.AttestationFilter{
			Contexts: []string{"env:dev"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test4
	})

	t.Run("filter by actors", func(t *testing.T) {
		// Test single actor using new Actors field
		results, err := store.GetAttestations(ats.AttestationFilter{
			Actors: []string{"service:api"},
			Limit:  10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Test multiple actors (OR logic)
		results, err = store.GetAttestations(ats.AttestationFilter{
			Actors: []string{"component:auth", "component:worker"},
			Limit:  10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 3) // test1, test2, and test4

		// Test backwards compatibility with single Actor field
		results, err = store.GetAttestations(ats.AttestationFilter{
			Actor: "service:web",
			Limit: 10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 1) // test2
	})

	t.Run("combined filters (AND logic between different fields)", func(t *testing.T) {
		// Filter by subject AND predicate
		results, err := store.GetAttestations(ats.AttestationFilter{
			Subjects:   []string{"user:alice"},
			Predicates: []string{"can:write"},
			Limit:      10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Filter by context AND actor
		results, err = store.GetAttestations(ats.AttestationFilter{
			Contexts: []string{"project:alpha"},
			Actors:   []string{"service:api"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test3

		// Complex filter combining multiple fields
		results, err = store.GetAttestations(ats.AttestationFilter{
			Subjects:   []string{"user:alice", "user:bob"},
			Predicates: []string{"can:read"},
			Contexts:   []string{"env:dev", "env:prod"},
			Limit:      10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 (alice + read + dev) and test2 (bob + read + prod)
	})

	t.Run("time range filtering", func(t *testing.T) {
		// Filter by time range
		oneHourAgo := now.Add(-1*time.Hour - 30*time.Minute)
		results, err := store.GetAttestations(ats.AttestationFilter{
			TimeStart: &oneHourAgo,
			Limit:     10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // test1 and test2

		// Combine time range with other filters
		results, err = store.GetAttestations(ats.AttestationFilter{
			Subjects:  []string{"user:alice"},
			TimeStart: &oneHourAgo,
			Limit:     10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 1) // only test1 (test3 is too old)
	})

	t.Run("empty filters return all", func(t *testing.T) {
		results, err := store.GetAttestations(ats.AttestationFilter{
			Limit: 10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 4) // all test data
	})

	t.Run("no matches return empty", func(t *testing.T) {
		results, err := store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{"user:nonexistent"},
			Limit:    10,
		})
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}