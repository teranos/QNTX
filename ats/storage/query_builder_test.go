package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teranos/QNTX/ats/types"
)

// TestBuildSubjectFilter tests subject filter SQL generation
func TestBuildSubjectFilter(t *testing.T) {
	testCases := []struct {
		name             string
		subjects         []string
		expectedClauses  int
		expectedArgs     int
		expectedContains string
	}{
		{
			name:             "empty subjects",
			subjects:         []string{},
			expectedClauses:  0,
			expectedArgs:     0,
			expectedContains: "",
		},
		{
			name:             "single subject",
			subjects:         []string{"Alice"},
			expectedClauses:  1,
			expectedArgs:     1,
			expectedContains: "attestation_subjects",
		},
		{
			name:             "multiple subjects",
			subjects:         []string{"Alice", "Bob"},
			expectedClauses:  1,
			expectedArgs:     2,
			expectedContains: "attestation_subjects",
		},
		{
			name:             "subject with special chars",
			subjects:         []string{"user_id"},
			expectedClauses:  1,
			expectedArgs:     1,
			expectedContains: "attestation_subjects",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			qb := &queryBuilder{}
			qb.buildSubjectFilter(tc.subjects)

			assert.Equal(t, tc.expectedClauses, len(qb.whereClauses), "Should have correct number of WHERE clauses")
			assert.Equal(t, tc.expectedArgs, len(qb.args), "Should have correct number of arguments")

			if tc.expectedContains != "" {
				assert.Contains(t, qb.whereClauses[0], tc.expectedContains, "Clause should contain expected SQL")
			}

			// Verify arguments are the raw subject values (exact match via junction table)
			for i, subject := range tc.subjects {
				assert.Equal(t, subject, qb.args[i], "Argument should be the exact subject value")
			}
		})
	}
}

// TestBuildPredicateFilter tests predicate filter SQL generation
func TestBuildPredicateFilter(t *testing.T) {
	testCases := []struct {
		name            string
		predicates      []string
		expectedClauses int
		expectedArgs    int
	}{
		{
			name:            "empty predicates",
			predicates:      []string{},
			expectedClauses: 0,
			expectedArgs:    0,
		},
		{
			name:            "single predicate",
			predicates:      []string{"works_at"},
			expectedClauses: 1,
			expectedArgs:    1,
		},
		{
			name:            "multiple predicates",
			predicates:      []string{"works_at", "lives_in"},
			expectedClauses: 1,
			expectedArgs:    2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			qb := &queryBuilder{}
			qb.buildPredicateFilter(tc.predicates)

			assert.Equal(t, tc.expectedClauses, len(qb.whereClauses))
			assert.Equal(t, tc.expectedArgs, len(qb.args))

			if len(tc.predicates) > 0 {
				assert.Contains(t, qb.whereClauses[0], "attestation_predicates")
			}
		})
	}
}

// TestBuildContextFilter tests context filter SQL generation
func TestBuildContextFilter(t *testing.T) {
	t.Run("multiple contexts with case-insensitive matching", func(t *testing.T) {
		qb := &queryBuilder{}
		qb.buildContextFilter([]string{"ACME Corp", "Engineering"})

		assert.Equal(t, 1, len(qb.whereClauses))
		assert.Equal(t, 2, len(qb.args))
		// Context matching should use junction table with COLLATE NOCASE
		assert.Contains(t, qb.whereClauses[0], "attestation_contexts")
		assert.Contains(t, qb.whereClauses[0], "COLLATE NOCASE")
	})

	t.Run("empty contexts", func(t *testing.T) {
		qb := &queryBuilder{}
		qb.buildContextFilter([]string{})

		assert.Equal(t, 0, len(qb.whereClauses))
		assert.Equal(t, 0, len(qb.args))
	})
}

// TestBuildActorFilter tests actor filter SQL generation
func TestBuildActorFilter(t *testing.T) {
	qb := &queryBuilder{}
	qb.buildActorFilter([]string{"user@example.com"})

	assert.Equal(t, 1, len(qb.whereClauses))
	assert.Equal(t, 1, len(qb.args))
	assert.Contains(t, qb.whereClauses[0], "attestation_actors")
}

// TestBuildTemporalFilters tests timestamp range filters
func TestBuildTemporalFilters(t *testing.T) {
	now := time.Now()
	later := now.Add(24 * time.Hour)

	t.Run("with start time", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			TimeStart: &now,
		}
		qb.buildTemporalFilters(filter)

		assert.Equal(t, 1, len(qb.whereClauses))
		assert.Contains(t, qb.whereClauses[0], "timestamp >")
		assert.Equal(t, now.UTC().Format(time.RFC3339), qb.args[0])
	})

	t.Run("with end time", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			TimeEnd: &later,
		}
		qb.buildTemporalFilters(filter)

		assert.Equal(t, 1, len(qb.whereClauses))
		assert.Contains(t, qb.whereClauses[0], "timestamp <=")
		assert.Equal(t, later.UTC().Format(time.RFC3339), qb.args[0])
	})

	t.Run("with both start and end", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			TimeStart: &now,
			TimeEnd:   &later,
		}
		qb.buildTemporalFilters(filter)

		assert.Equal(t, 2, len(qb.whereClauses))
		assert.Equal(t, 2, len(qb.args))
	})

	t.Run("with neither", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{}
		qb.buildTemporalFilters(filter)

		assert.Equal(t, 0, len(qb.whereClauses))
		assert.Equal(t, 0, len(qb.args))
	})
}

// TestAddClause tests the helper method
func TestAddClause(t *testing.T) {
	qb := &queryBuilder{}

	qb.addClause("test = ?", "value1")
	qb.addClause("another = ?", "value2")

	assert.Equal(t, 2, len(qb.whereClauses))
	assert.Equal(t, 2, len(qb.args))
	assert.Equal(t, "test = ?", qb.whereClauses[0])
	assert.Equal(t, "another = ?", qb.whereClauses[1])
	assert.Equal(t, "value1", qb.args[0])
	assert.Equal(t, "value2", qb.args[1])
}
