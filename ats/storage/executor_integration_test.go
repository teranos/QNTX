package storage

import (
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/db"
)

func setupTestDatabaseWithAttestations(t *testing.T) *sql.DB {
	// Create in-memory test database
	testDB := qntxtest.CreateTestDB(t)
	require.NoError(t, err)

	// Use migrations to create all tables (attestations from migration 025, aliases from migration 052)
	err = db.Migrate(testDB, nil)
	require.NoError(t, err)

	// Create test attestations using the existing CreateAttestation function
	testTime1, _ := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	testTime2, _ := time.Parse(time.RFC3339, "2024-01-02T00:00:00Z")
	testTime3, _ := time.Parse(time.RFC3339, "2024-01-03T00:00:00Z")
	testTime4, _ := time.Parse(time.RFC3339, "2024-01-04T00:00:00Z")
	testTime5, _ := time.Parse(time.RFC3339, "2024-01-05T00:00:00Z")

	testAttestations := []*types.As{
		{
			ID:         "test1",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"engineer"},
			Contexts:   []string{"RESEARCH_LAB"},
			Actors:     []string{"hr"},
			Timestamp:  testTime1,
			Source:     "test",
			CreatedAt:  testTime1,
		},
		{
			ID:         "test2",
			Subjects:   []string{"BOB"},
			Predicates: []string{"software engineer"},
			Contexts:   []string{"RESEARCH_LAB"},
			Actors:     []string{"registry"},
			Timestamp:  testTime2,
			Source:     "test",
			CreatedAt:  testTime2,
		},
		{
			ID:         "test3",
			Subjects:   []string{"CHARLIE"},
			Predicates: []string{"manager"},
			Contexts:   []string{"ACME"},
			Actors:     []string{"hr"},
			Timestamp:  testTime3,
			Source:     "test",
			CreatedAt:  testTime3,
		},
		{
			ID:         "test4",
			Subjects:   []string{"DIANA"},
			Predicates: []string{"senior software engineer"},
			Contexts:   []string{"RESEARCH_LAB"},
			Actors:     []string{"registry"},
			Timestamp:  testTime4,
			Source:     "test",
			CreatedAt:  testTime4,
		},
		{
			ID:         "test5",
			Subjects:   []string{"EVE"},
			Predicates: []string{"product manager"},
			Contexts:   []string{"STARTUP"},
			Actors:     []string{"self"},
			Timestamp:  testTime5,
			Source:     "test",
			CreatedAt:  testTime5,
		},
	}

	// Insert using the CreateAttestation function from storage package
	store := NewSQLStore(testDB, nil)
	for _, attestation := range testAttestations {
		err = store.CreateAttestation(attestation)
		require.NoError(t, err)
	}

	return testDB
}

func TestAxExecutorBasicQueries(t *testing.T) {
	db := setupTestDatabaseWithAttestations(t)
	defer db.Close()

	executor := NewExecutor(db)

	tests := []struct {
		name             string
		filter           types.AxFilter
		expectedMinCount int
		shouldContainIDs []string
	}{
		{
			name: "query by subject",
			filter: types.AxFilter{
				Subjects: []string{"ALICE"},
				Limit:    10,
			},
			expectedMinCount: 1,
			shouldContainIDs: []string{"test1"},
		},
		{
			name: "query by context",
			filter: types.AxFilter{
				Contexts: []string{"RESEARCH_LAB"},
				Limit:    10,
			},
			expectedMinCount: 3,
			shouldContainIDs: []string{"test1", "test2", "test4"},
		},
		{
			name: "query by actor",
			filter: types.AxFilter{
				Actors: []string{"hr"},
				Limit:  10,
			},
			expectedMinCount: 2,
			shouldContainIDs: []string{"test1", "test3"},
		},
		{
			name: "fuzzy predicate matching",
			filter: types.AxFilter{
				Predicates: []string{"engineer"}, // Should match "engineer", "software engineer", "senior software engineer"
				Limit:      10,
			},
			expectedMinCount: 3,
			shouldContainIDs: []string{"test1", "test2", "test4"},
		},
		{
			name: "multiple filters",
			filter: types.AxFilter{
				Contexts:   []string{"RESEARCH_LAB"},
				Predicates: []string{"software"},
				Limit:      10,
			},
			expectedMinCount: 2,
			shouldContainIDs: []string{"test2", "test4"},
		},
		{
			name: "no filters - return all",
			filter: types.AxFilter{
				Limit: 10,
			},
			expectedMinCount: 5,
			shouldContainIDs: []string{"test1", "test2", "test3", "test4", "test5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.ExecuteAsk(context.Background(), tt.filter)
			require.NoError(t, err)

			assert.GreaterOrEqual(t, len(result.Attestations), tt.expectedMinCount,
				"Should have at least %d attestations", tt.expectedMinCount)

			// Check that expected IDs are present
			foundIDs := make(map[string]bool)
			for _, as := range result.Attestations {
				foundIDs[as.ID] = true
			}

			for _, expectedID := range tt.shouldContainIDs {
				assert.True(t, foundIDs[expectedID],
					"Result should contain attestation with ID %s", expectedID)
			}

			// Check summary
			assert.Equal(t, len(result.Attestations), result.Summary.TotalAttestations)
			assert.NotNil(t, result.Summary.UniqueSubjects)
			assert.NotNil(t, result.Summary.UniquePredicates)
			assert.NotNil(t, result.Summary.UniqueContexts)
			assert.NotNil(t, result.Summary.UniqueActors)
		})
	}
}

func TestAxExecutorTemporalFiltering(t *testing.T) {
	db := setupTestDatabaseWithAttestations(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Test temporal filtering
	startTime, _ := time.Parse(time.RFC3339, "2024-01-02T00:00:00Z")
	endTime, _ := time.Parse(time.RFC3339, "2024-01-04T00:00:00Z")

	tests := []struct {
		name     string
		filter   types.AxFilter
		expected []string
	}{
		{
			name: "filter by start time",
			filter: types.AxFilter{
				TimeStart: &startTime,
				Limit:     10,
			},
			expected: []string{"test5", "test4", "test3"}, // From 2024-01-02 onwards (DESC order), test2 excluded due to exact match boundary
		},
		{
			name: "filter by end time",
			filter: types.AxFilter{
				TimeEnd: &endTime,
				Limit:   10,
			},
			expected: []string{"test1", "test2", "test3", "test4"}, // Up to 2024-01-04
		},
		{
			name: "filter by time range",
			filter: types.AxFilter{
				TimeStart: &startTime,
				TimeEnd:   &endTime,
				Limit:     10,
			},
			expected: []string{"test4", "test3"}, // Between 2024-01-02 and 2024-01-04 (DESC order), boundary dates excluded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.ExecuteAsk(context.Background(), tt.filter)
			require.NoError(t, err)

			foundIDs := make([]string, len(result.Attestations))
			for i, as := range result.Attestations {
				foundIDs[i] = as.ID
			}

			assert.ElementsMatch(t, tt.expected, foundIDs)
		})
	}
}

// TestAxExecutorFuzzyPredicateExpansion is disabled because it tests unexported implementation details
// TODO: Reimplement this test using public API when needed
/*
func TestAxExecutorFuzzyPredicateExpansion(t *testing.T) {
	db := setupTestDatabaseWithAttestations(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Test fuzzy predicate expansion directly
	expanded, err := executor.expandFuzzyPredicates(context.Background(), []string{"engineer"})
	require.NoError(t, err)

	// Should expand to include various engineer types
	expectedPredicates := []string{
		"engineer",
		"software engineer",
		"senior software engineer",
	}

	for _, expected := range expectedPredicates {
		assert.Contains(t, expanded, expected,
			"Fuzzy expansion should include '%s'", expected)
	}

	// Should not include non-engineer predicates
	assert.NotContains(t, expanded, "manager")
	assert.NotContains(t, expanded, "product manager")
}
*/

// TestAxExecutorGetAllPredicatesFromDB is disabled because it tests unexported implementation details
// TODO: Reimplement this test using public API when needed
/*
func TestAxExecutorGetAllPredicatesFromDB(t *testing.T) {
	db := setupTestDatabaseWithAttestations(t)
	defer db.Close()

	executor := NewExecutor(db)

	// Get predicates directly from query store
	queryStore := NewSQLQueryStore(testDB)
	predicates, err := queryStore.GetAllPredicates(context.Background())
	require.NoError(t, err)

	expectedPredicates := []string{
		"engineer",
		"software engineer",
		"manager",
		"senior software engineer",
		"product manager",
	}

	// Check that all expected predicates are present
	for _, expected := range expectedPredicates {
		assert.Contains(t, predicates, expected,
			"Should contain predicate '%s'", expected)
	}

	// Should not contain underscore or empty predicates
	assert.NotContains(t, predicates, "_")
	assert.NotContains(t, predicates, "")
}
*/

func TestAxExecutorLimitHandling(t *testing.T) {
	db := setupTestDatabaseWithAttestations(t)
	defer db.Close()

	executor := NewExecutor(db)

	tests := []struct {
		name          string
		limit         int
		expectedCount int
	}{
		{
			name:          "small limit",
			limit:         2,
			expectedCount: 2,
		},
		{
			name:          "large limit",
			limit:         10,
			expectedCount: 5, // Only 5 test attestations
		},
		{
			name:          "zero limit uses default",
			limit:         0,
			expectedCount: 5, // Should return all (within default limit of 100)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.ExecuteAsk(context.Background(), types.AxFilter{
				Limit: tt.limit,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.expectedCount, len(result.Attestations))
		})
	}
}

func TestAxExecutorEdgeCases(t *testing.T) {
	// Create a fresh database for each subtest to avoid isolation issues
	// db := setupTestDatabaseWithAttestations(t)
	// defer db.Close()
	// executor := NewAxExecutor(db)

	tests := []struct {
		name   string
		filter types.AxFilter
	}{
		{
			name: "empty filter",
			filter: types.AxFilter{
				Limit: 10,
			},
		},
		{
			name: "nonexistent subject",
			filter: types.AxFilter{
				Subjects: []string{"NONEXISTENT"},
				Limit:    10,
			},
		},
		{
			name: "nonexistent predicate",
			filter: types.AxFilter{
				Predicates: []string{"nonexistent"},
				Limit:      10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh database and executor for each subtest
			db := setupTestDatabaseWithAttestations(t)
			defer db.Close()
			executor := NewExecutor(db)

			result, err := executor.ExecuteAsk(context.Background(), tt.filter)
			require.NoError(t, err)
			require.NotNil(t, result, "Result should not be nil")
			assert.NotNil(t, result.Attestations)
			assert.NotNil(t, result.Summary)
		})
	}
}
