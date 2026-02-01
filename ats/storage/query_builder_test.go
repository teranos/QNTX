package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// TestEscapeLikePattern tests SQL LIKE pattern escaping
func TestEscapeLikePattern(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal text",
			input:    "normal",
			expected: "normal",
		},
		{
			name:     "text with underscore",
			input:    "test_value",
			expected: "test\\_value",
		},
		{
			name:     "text with percent",
			input:    "test%value",
			expected: "test\\%value",
		},
		{
			name:     "text with backslash",
			input:    "test\\value",
			expected: "test\\\\value",
		},
		{
			name:     "multiple special chars",
			input:    "test%value_with\\backslash",
			expected: "test\\%value\\_with\\\\backslash",
		},
		{
			name:     "SQL injection attempt",
			input:    "'; DROP TABLE attestations; --",
			expected: "'; DROP TABLE attestations; --",
		},
		{
			name:     "wildcard injection attempt",
			input:    "user%' OR '1'='1",
			expected: "user\\%' OR '1'='1",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := escapeLikePattern(tc.input)
			assert.Equal(t, tc.expected, result, "Escaping should match expected output")
		})
	}
}

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
			expectedContains: "subjects LIKE ? ESCAPE",
		},
		{
			name:             "multiple subjects",
			subjects:         []string{"Alice", "Bob"},
			expectedClauses:  1,
			expectedArgs:     2,
			expectedContains: "OR",
		},
		{
			name:             "subject with special chars",
			subjects:         []string{"user_id"},
			expectedClauses:  1,
			expectedArgs:     1,
			expectedContains: "subjects LIKE ? ESCAPE",
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

			// Verify arguments are properly escaped JSON patterns
			for i, subject := range tc.subjects {
				if len(tc.subjects) > 0 {
					expected := "%\"" + escapeLikePattern(subject) + "\"%"
					assert.Equal(t, expected, qb.args[i], "Argument should be escaped JSON pattern")
				}
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
				assert.Contains(t, qb.whereClauses[0], "predicates LIKE ? ESCAPE")
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
		// Context matching should be case-insensitive (COLLATE NOCASE)
		assert.Contains(t, qb.whereClauses[0], "contexts LIKE ? COLLATE NOCASE ESCAPE")
		assert.Contains(t, qb.whereClauses[0], "OR")
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
	assert.Contains(t, qb.whereClauses[0], "actors LIKE ? ESCAPE")
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
		assert.Equal(t, &now, qb.args[0])
	})

	t.Run("with end time", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			TimeEnd: &later,
		}
		qb.buildTemporalFilters(filter)

		assert.Equal(t, 1, len(qb.whereClauses))
		assert.Contains(t, qb.whereClauses[0], "timestamp <=")
		assert.Equal(t, &later, qb.args[0])
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

// TestBuildOverComparisonFilter tests OVER numeric comparison filters
func TestBuildOverComparisonFilter(t *testing.T) {
	mockExpander := &mockQueryExpander{
		numericPredicates: []string{"experience_years", "tenure_years"},
	}

	t.Run("pure OVER query (no other clauses)", func(t *testing.T) {
		qb := &queryBuilder{}
		overFilter := &types.OverFilter{
			Value:    5.0,
			Unit:     "y",
			Operator: "over",
		}

		err := qb.buildOverComparisonFilter(mockExpander, overFilter, false, types.AxFilter{})
		assert.NoError(t, err)

		assert.Equal(t, 1, len(qb.whereClauses), "Should add one WHERE clause")
		assert.Contains(t, qb.whereClauses[0], "json_extract(predicates, '$[0]')")
		assert.Contains(t, qb.whereClauses[0], "CAST(json_extract(contexts, '$[0]') AS REAL) >=")
		// Should have 2 predicates * 2 args each (predicate name + threshold) = 4 args
		assert.Equal(t, 4, len(qb.args))
	})

	t.Run("combined query (with other clauses)", func(t *testing.T) {
		qb := &queryBuilder{}
		qb.whereClauses = []string{"subjects LIKE ?"} // Simulate existing clause
		overFilter := &types.OverFilter{
			Value:    5.0,
			Unit:     "y",
			Operator: "over",
		}

		err := qb.buildOverComparisonFilter(mockExpander, overFilter, true, types.AxFilter{})
		assert.NoError(t, err)

		assert.Equal(t, 2, len(qb.whereClauses), "Should add clause to existing ones")
		assert.Contains(t, qb.whereClauses[1], "SELECT DISTINCT")
		assert.Contains(t, qb.whereClauses[1], "json_extract(subjects, '$[0]') IN")
	})

	t.Run("converts months to years", func(t *testing.T) {
		qb := &queryBuilder{}
		overFilter := &types.OverFilter{
			Value:    24.0,
			Unit:     "m",
			Operator: "over",
		}

		err := qb.buildOverComparisonFilter(mockExpander, overFilter, false, types.AxFilter{})
		assert.NoError(t, err)

		// Should convert 24 months to 2 years
		// Last arg should be the threshold (2.0)
		lastArg := qb.args[len(qb.args)-1]
		assert.Equal(t, 2.0, lastArg, "Should convert months to years (24m = 2y)")
	})

	t.Run("nil OVER filter", func(t *testing.T) {
		qb := &queryBuilder{}
		err := qb.buildOverComparisonFilter(mockExpander, nil, false, types.AxFilter{})
		assert.NoError(t, err)

		assert.Equal(t, 0, len(qb.whereClauses))
		assert.Equal(t, 0, len(qb.args))
	})

	t.Run("no numeric predicates", func(t *testing.T) {
		emptyExpander := &mockQueryExpander{
			numericPredicates: []string{},
		}
		qb := &queryBuilder{}
		overFilter := &types.OverFilter{
			Value:    5.0,
			Unit:     "y",
			Operator: "over",
		}

		err := qb.buildOverComparisonFilter(emptyExpander, overFilter, false, types.AxFilter{})
		assert.NoError(t, err)

		assert.Equal(t, 0, len(qb.whereClauses), "Should skip when no numeric predicates defined")
	})
}

// TestBuildNaturalLanguageFilter tests NL query expansion
func TestBuildNaturalLanguageFilter(t *testing.T) {
	mockExpander := &mockQueryExpander{
		nlPredicates: []string{"is"},
		expansions: []ats.PredicateExpansion{
			{Predicate: "role", Context: "engineer"},
			{Predicate: "title", Context: "engineer"},
		},
	}

	t.Run("natural language expansion", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			Predicates: []string{"is", "engineer"},
		}

		qb.buildNaturalLanguageFilter(mockExpander, filter)

		assert.Equal(t, 1, len(qb.whereClauses))
		assert.Contains(t, qb.whereClauses[0], "predicates LIKE ? ESCAPE")
		assert.Contains(t, qb.whereClauses[0], "contexts LIKE ? COLLATE NOCASE ESCAPE")
		assert.Contains(t, qb.whereClauses[0], "AND")
		assert.Contains(t, qb.whereClauses[0], "OR")
	})

	t.Run("empty predicates", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			Predicates: []string{},
		}

		qb.buildNaturalLanguageFilter(mockExpander, filter)

		assert.Equal(t, 0, len(qb.whereClauses))
	})

	t.Run("with explicit contexts", func(t *testing.T) {
		qb := &queryBuilder{}
		filter := types.AxFilter{
			Predicates: []string{"is", "engineer"},
			Contexts:   []string{"ACME Corp"},
		}

		qb.buildNaturalLanguageFilter(mockExpander, filter)

		// Should have clauses for both NL expansion and explicit contexts
		assert.GreaterOrEqual(t, len(qb.whereClauses), 1)
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

// mockQueryExpander for testing
type mockQueryExpander struct {
	numericPredicates []string
	nlPredicates      []string
	expansions        []ats.PredicateExpansion
}

func (m *mockQueryExpander) ExpandPredicate(predicate string, values []string) []ats.PredicateExpansion {
	return m.expansions
}

func (m *mockQueryExpander) GetNumericPredicates() []string {
	return m.numericPredicates
}

func (m *mockQueryExpander) GetNaturalLanguagePredicates() []string {
	return m.nlPredicates
}
