package storage

import (
	"context"
	"database/sql"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
)

// testDomainExpander provides domain-specific query expansion for tests
// This demonstrates how domains can extend ATS with custom semantic expansion
type testDomainExpander struct{}

func (e *testDomainExpander) ExpandPredicate(predicate string, values []string) []ats.PredicateExpansion {
	var expansions []ats.PredicateExpansion

	for _, value := range values {
		// Always include literal match
		expansions = append(expansions, ats.PredicateExpansion{
			Predicate: predicate,
			Context:   value,
		})

		// Semantic mappings for "is" queries (e.g., "is type_a")
		if predicate == "is" {
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "category",
				Context:   value,
			})
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: "has_attribute",
				Context:   value,
			})
		}

		// Normalize verb variations
		normalizedPred := normalizePredicate(predicate)
		if normalizedPred != predicate {
			expansions = append(expansions, ats.PredicateExpansion{
				Predicate: normalizedPred,
				Context:   value,
			})
		}
	}

	return expansions
}

func (e *testDomainExpander) GetNumericPredicates() []string {
	return []string{
		"total_duration",
		"duration_in_role_a",
		"duration_in_role_b",
		"duration_in_role_c",
		"overall_duration",
	}
}

func (e *testDomainExpander) GetNaturalLanguagePredicates() []string {
	return []string{
		"is", "has",
		"speak", "speaks",
		"know", "knows",
		"work", "worked",
		"study", "studied",
		"lives_in",
		"over",
	}
}

func normalizePredicate(pred string) string {
	switch pred {
	case "speak":
		return "speaks"
	case "know":
		return "knows"
	case "work":
		return "worked"
	case "study":
		return "studied"
	default:
		return pred
	}
}

// setupDomainTestDB creates a test database with recruiting-domain attestations.
// Uses real migrations to ensure test schema matches production schema.
func setupDomainTestDB(t *testing.T) *sql.DB {
	// Create in-memory test database
	testDB := qntxtest.CreateTestDB(t)

	// Create test attestations with predicate-context pairs
	testTime := time.Now()

	testAttestations := []*types.As{
		// Entity 1 - Dutch speaker, Senior Engineer
		{
			ID:         "TEST001",
			Subjects:   []string{"KEES"},
			Predicates: []string{"speaks"},
			Contexts:   []string{"Dutch"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST002",
			Subjects:   []string{"KEES"},
			Predicates: []string{"speaks"},
			Contexts:   []string{"English"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST003",
			Subjects:   []string{"KEES"},
			Predicates: []string{"occupation"},
			Contexts:   []string{"Senior DevOps Engineer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST004",
			Subjects:   []string{"KEES"},
			Predicates: []string{"has_experience"},
			Contexts:   []string{"DevOps Engineer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		// Semantic bindings derived from "Senior DevOps Engineer" occupation
		{
			ID:         "TEST004A",
			Subjects:   []string{"KEES"},
			Predicates: []string{"is"},
			Contexts:   []string{"engineer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST004B",
			Subjects:   []string{"KEES"},
			Predicates: []string{"is"},
			Contexts:   []string{"developer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST004C",
			Subjects:   []string{"KEES"},
			Predicates: []string{"is"},
			Contexts:   []string{"senior_level"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST005",
			Subjects:   []string{"KEES"},
			Predicates: []string{"duration_in_role_a"},
			Contexts:   []string{"8.0"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST006",
			Subjects:   []string{"KEES"},
			Predicates: []string{"total_duration"},
			Contexts:   []string{"10.0"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		// Entity 2 - English only, Junior Developer
		{
			ID:         "TEST007",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"speaks"},
			Contexts:   []string{"English"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST008",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"occupation"},
			Contexts:   []string{"Junior Software Developer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		// Semantic bindings derived from "Junior Software Developer" occupation
		{
			ID:         "TEST008A",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"is"},
			Contexts:   []string{"engineer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST008B",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"is"},
			Contexts:   []string{"developer"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST008C",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"is"},
			Contexts:   []string{"junior_level"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST009",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"duration_in_role_a"},
			Contexts:   []string{"2.0"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST010",
			Subjects:   []string{"JOHN"},
			Predicates: []string{"total_duration"},
			Contexts:   []string{"2.0"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		// Entity 3 - Dutch speaker, Product Manager (non-engineer)
		{
			ID:         "TEST011",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"speaks"},
			Contexts:   []string{"Dutch"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST012",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"speaks"},
			Contexts:   []string{"English"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST013",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"occupation"},
			Contexts:   []string{"Product Manager"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		// Semantic bindings derived from "Product Manager" occupation (manager but NOT engineer)
		{
			ID:         "TEST013A",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"is"},
			Contexts:   []string{"manager"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST013B",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"is"},
			Contexts:   []string{"leadership_role"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
		{
			ID:         "TEST014",
			Subjects:   []string{"ANNA"},
			Predicates: []string{"total_duration"},
			Contexts:   []string{"6.0"},
			Actors:     []string{"test@user"},
			Timestamp:  testTime,
			Source:     "test",
		},
	}

	// Insert attestations
	store := NewSQLStore(testDB, nil)
	for _, attestation := range testAttestations {
		err := store.CreateAttestation(attestation)
		require.NoError(t, err)
	}

	return testDB
}

// TestNaturalLanguageQueries tests the natural language query enhancements
func TestNaturalLanguageQueries(t *testing.T) {
	db := setupDomainTestDB(t)

	// Create executor with query expander for semantic expansion
	expander := &testDomainExpander{}
	executor := NewExecutorWithOptions(db, ax.AxExecutorOptions{
		QueryExpander: expander,
	})

	t.Run("speaks dutch - validates entity attribute data", func(t *testing.T) {
		filter := types.AxFilter{
			Predicates: []string{"speaks", "dutch"},
			Limit:      10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find KEES and ANNA who speak Dutch (case-insensitive matching tested separately)
		assert.Len(t, result.Attestations, 2)

		subjects := make(map[string]bool)
		for _, att := range result.Attestations {
			if len(att.Subjects) > 0 {
				subjects[att.Subjects[0]] = true
			}
		}

		assert.True(t, subjects["KEES"], "Should find KEES who speaks Dutch")
		assert.True(t, subjects["ANNA"], "Should find ANNA who speaks Dutch")
		assert.False(t, subjects["JOHN"], "Should NOT find JOHN (English only)")
	})

	t.Run("speak dutch - singular form works", func(t *testing.T) {
		filter := types.AxFilter{
			Predicates: []string{"speak", "dutch"},
			Limit:      10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find same results as "speaks dutch"
		assert.Len(t, result.Attestations, 2)
	})

	t.Run("is engineer - validates semantic occupation mapping", func(t *testing.T) {
		filter := types.AxFilter{
			Predicates: []string{"is", "engineer"},
			Limit:      10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find KEES (Senior DevOps Engineer) and JOHN (Junior Software Developer)
		// but not ANNA (Product Manager) - tests semantic "is" mapping to occupation data
		subjects := make(map[string]bool)
		for _, att := range result.Attestations {
			if len(att.Subjects) > 0 {
				subjects[att.Subjects[0]] = true
			}
		}

		assert.True(t, subjects["KEES"], "Should find KEES (DevOps Engineer)")
		assert.False(t, subjects["ANNA"], "Should NOT find ANNA (Product Manager, not engineer)")
	})

	t.Run("over 5y - finds entities with sufficient duration", func(t *testing.T) {
		filter := types.AxFilter{
			OverComparison: &types.OverFilter{
				Value:    5,
				Unit:     "y",
				Operator: "over",
			},
			// OverComparison queries have automatic safety limit (10000) for memory protection
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find KEES (10 years) and ANNA (6 years)
		// but not JOHN (2 years)
		subjects := make(map[string]bool)
		for _, att := range result.Attestations {
			if len(att.Subjects) > 0 {
				subjects[att.Subjects[0]] = true
			}
		}

		assert.True(t, subjects["KEES"], "Should find KEES with 10 years duration")
		assert.True(t, subjects["ANNA"], "Should find ANNA with 6 years duration")
		assert.False(t, subjects["JOHN"], "Should NOT find JOHN with only 2 years duration")
	})

	t.Run("is engineer over 5y - combines predicate and duration filters", func(t *testing.T) {
		filter := types.AxFilter{
			Predicates: []string{"is", "engineer"},
			OverComparison: &types.OverFilter{
				Value:    5,
				Unit:     "y",
				Operator: "over",
			},
			Limit: 10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find only KEES (engineer with 10 years)
		// Not ANNA (6 years but Product Manager, not engineer)
		// Not JOHN (engineer but only 2 years)
		subjects := make(map[string]bool)
		for _, att := range result.Attestations {
			if len(att.Subjects) > 0 {
				subjects[att.Subjects[0]] = true
			}
		}

		assert.True(t, subjects["KEES"], "Should find KEES - engineer with 10 years")
		assert.False(t, subjects["ANNA"], "Should NOT find ANNA - not an engineer")
		assert.False(t, subjects["JOHN"], "Should NOT find JOHN - only 2 years duration")
	})
}

// TestOverQueryParsing tests the parsing of "over" queries
func TestOverQueryParsing(t *testing.T) {
	t.Run("parse over 5y", func(t *testing.T) {
		args := []string{"is", "engineer", "over", "5y"}
		filter, err := parser.ParseAxCommand(args)

		// May have warnings, but should parse
		if err != nil {
			if pw, ok := err.(*parser.ParseWarning); ok {
				filter = pw.Filter
			} else {
				require.NoError(t, err)
			}
		}

		assert.NotNil(t, filter.OverComparison)
		assert.Equal(t, 5.0, filter.OverComparison.Value)
		assert.Equal(t, "y", filter.OverComparison.Unit)
		assert.Equal(t, "over", filter.OverComparison.Operator)
		assert.Equal(t, []string{"engineer"}, filter.Predicates)
	})

	t.Run("parse over 6m", func(t *testing.T) {
		args := []string{"speaks", "dutch", "over", "6m"}
		filter, err := parser.ParseAxCommand(args)

		if err != nil {
			if pw, ok := err.(*parser.ParseWarning); ok {
				filter = pw.Filter
			} else {
				require.NoError(t, err)
			}
		}

		assert.NotNil(t, filter.OverComparison)
		assert.Equal(t, 6.0, filter.OverComparison.Value)
		assert.Equal(t, "m", filter.OverComparison.Unit)
	})

	t.Run("error on missing unit", func(t *testing.T) {
		args := []string{"is", "engineer", "over", "5"}
		_, err := parser.ParseAxCommand(args)

		// Should have a warning about missing unit
		if pw, ok := err.(*parser.ParseWarning); ok {
			assert.Contains(t, strings.Join(pw.Warnings, " "), "5y")
		}
	})

	t.Run("over with so action", func(t *testing.T) {
		args := []string{"is", "engineer", "over", "5y", "so", "export", "csv"}
		filter, err := parser.ParseAxCommand(args)

		if err != nil {
			if pw, ok := err.(*parser.ParseWarning); ok {
				filter = pw.Filter
			} else {
				require.NoError(t, err)
			}
		}

		assert.NotNil(t, filter.OverComparison)
		assert.Equal(t, 5.0, filter.OverComparison.Value)
		assert.Equal(t, []string{"export", "csv"}, filter.SoActions)
	})
}

// TestPredicateContextMatching tests the predicate+context matching logic
func TestPredicateContextMatching(t *testing.T) {
	db := setupDomainTestDB(t)

	// Create executor with query expander for semantic expansion
	expander := &testDomainExpander{}
	executor := NewExecutorWithOptions(db, ax.AxExecutorOptions{
		QueryExpander: expander,
	})

	t.Run("direct predicate-context pair matching", func(t *testing.T) {
		// This should match predicates="speaks" AND contexts contains "dutch"
		filter := types.AxFilter{
			Predicates: []string{"speaks", "dutch"},
			Limit:      10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Check that we get the right attestations
		for _, att := range result.Attestations {
			if len(att.Predicates) > 0 && att.Predicates[0] == "speaks" {
				// If predicate is "speaks", context should contain "Dutch"
				assert.Contains(t, att.Contexts, "Dutch")
			}
		}
	})

	t.Run("semantic predicate mapping for is queries", func(t *testing.T) {
		// "is engineer" should match occupation/has_experience predicates
		// with contexts containing "engineer"
		filter := types.AxFilter{
			Predicates: []string{"is", "developer"},
			Limit:      10,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err)

		// Should find both KEES and JOHN who have "Developer" in their occupation
		assert.Greater(t, len(result.Attestations), 0)
	})
}

// TestCaseInsensitiveContextMatching demonstrates the case-insensitive COLLATE functionality
// This test showcases how context queries now work regardless of casing differences
func TestCaseInsensitiveContextMatching(t *testing.T) {
	// Create test database with mixed case data to demonstrate case-insensitive matching
	testDB := qntxtest.CreateTestDB(t)

	// Insert test data with intentionally mixed casing
	testTime := time.Now()
	testAttestations := []*types.As{
		// Language attestations with mixed casing - this demonstrates the core use case
		{ID: "CASE001", Subjects: []string{"ALICE"}, Predicates: []string{"speaks"}, Contexts: []string{"Dutch"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // Capital D
		{ID: "CASE002", Subjects: []string{"BOB"}, Predicates: []string{"speaks"}, Contexts: []string{"dutch"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // lowercase d
		{ID: "CASE003", Subjects: []string{"CHARLIE"}, Predicates: []string{"speaks"}, Contexts: []string{"DUTCH"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // all caps
		{ID: "CASE004", Subjects: []string{"DIANA"}, Predicates: []string{"speaks"}, Contexts: []string{"English"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // Capital E
		{ID: "CASE005", Subjects: []string{"EVE"}, Predicates: []string{"speaks"}, Contexts: []string{"english"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // lowercase e

		// Location attestations with mixed casing
		{ID: "CASE006", Subjects: []string{"ALICE"}, Predicates: []string{"lives_in"}, Contexts: []string{"Amsterdam"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // Capital A
		{ID: "CASE007", Subjects: []string{"BOB"}, Predicates: []string{"lives_in"}, Contexts: []string{"amsterdam"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // lowercase a
		{ID: "CASE008", Subjects: []string{"CHARLIE"}, Predicates: []string{"lives_in"}, Contexts: []string{"AMSTERDAM"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // all caps

		// Profession attestations with mixed casing
		{ID: "CASE009", Subjects: []string{"ALICE"}, Predicates: []string{"is"}, Contexts: []string{"Engineer"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // Capital E
		{ID: "CASE010", Subjects: []string{"BOB"}, Predicates: []string{"is"}, Contexts: []string{"engineer"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // lowercase e
		{ID: "CASE011", Subjects: []string{"CHARLIE"}, Predicates: []string{"is"}, Contexts: []string{"ENGINEER"},
			Actors: []string{"test"}, Timestamp: testTime, Source: "test"}, // all caps
	}

	// Insert all test attestations
	store := NewSQLStore(testDB, nil)
	for _, att := range testAttestations {
		err := store.CreateAttestation(att)
		require.NoError(t, err)
	}

	// Create executor with query expander for semantic expansion
	expander := &testDomainExpander{}
	executor := NewExecutorWithOptions(testDB, ax.AxExecutorOptions{
		QueryExpander: expander,
	})

	testCases := []struct {
		name          string
		filter        types.AxFilter
		expectedCount int
		expectedNames []string
		description   string
	}{
		{
			name: "Dutch speakers - lowercase query matches all case variations",
			filter: types.AxFilter{
				Predicates: []string{"speaks", "dutch"},
				Limit:      10,
			},
			expectedCount: 3, // Should find Dutch, dutch, DUTCH
			expectedNames: []string{"ALICE", "BOB", "CHARLIE"},
			description:   "Query 'speaks dutch' should match Dutch/dutch/DUTCH",
		},
		{
			name: "Amsterdam residents - capital query matches all case variations",
			filter: types.AxFilter{
				Predicates: []string{"lives_in", "Amsterdam"},
				Limit:      10,
			},
			expectedCount: 3, // Should find Amsterdam, amsterdam, AMSTERDAM
			expectedNames: []string{"ALICE", "BOB", "CHARLIE"},
			description:   "Query 'lives_in Amsterdam' should match all case variations",
		},
		{
			name: "Engineers - mixed case query matches all case variations",
			filter: types.AxFilter{
				Predicates: []string{"is", "engineer"},
				Limit:      10,
			},
			expectedCount: 3, // Should find Engineer, engineer, ENGINEER
			expectedNames: []string{"ALICE", "BOB", "CHARLIE"},
			description:   "Query 'is engineer' should match all Engineer case variations",
		},
		{
			name: "English speakers - verify case-insensitive works for other languages too",
			filter: types.AxFilter{
				Predicates: []string{"speaks", "ENGLISH"}, // All caps query
				Limit:      10,
			},
			expectedCount: 2, // Should find English, english
			expectedNames: []string{"DIANA", "EVE"},
			description:   "Query 'speaks ENGLISH' should match English/english",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("=== Testing Case-Insensitive Query: %s ===", tc.description)

			// Execute the query
			result, err := executor.ExecuteAsk(context.Background(), tc.filter)
			require.NoError(t, err, "Query should execute without error")

			// Verify result count
			assert.Equal(t, tc.expectedCount, len(result.Attestations),
				"Expected %d results for query, got %d",
				tc.expectedCount, len(result.Attestations))

			// Extract subject names from results
			foundNames := make(map[string]bool)
			for _, att := range result.Attestations {
				for _, subject := range att.Subjects {
					foundNames[subject] = true
				}
			}

			// Verify all expected names were found
			for _, expectedName := range tc.expectedNames {
				assert.True(t, foundNames[expectedName],
					"Expected to find %s in results", expectedName)
			}

			// Log results for visibility
			t.Logf("✓ Case-insensitive query correctly matched %d results: %v",
				len(result.Attestations), tc.expectedNames)

			// Log a few example matches for verification
			for i, att := range result.Attestations {
				if i < 3 { // Show first 3 matches
					t.Logf("  Match %d: %v | %v | %v",
						i+1, att.Subjects, att.Predicates, att.Contexts)
				}
			}
		})
	}

	// Additional test: verify that case-insensitive queries return consistent results
	t.Run("Case variation consistency check", func(t *testing.T) {
		// Test that different case variations of the same query return the same number of results
		queries := []types.AxFilter{
			{Predicates: []string{"speaks", "dutch"}, Limit: 10}, // lowercase
			{Predicates: []string{"speaks", "Dutch"}, Limit: 10}, // capital
			{Predicates: []string{"speaks", "DUTCH"}, Limit: 10}, // uppercase
		}

		var resultCounts []int
		for _, filter := range queries {
			result, err := executor.ExecuteAsk(context.Background(), filter)
			require.NoError(t, err)
			resultCounts = append(resultCounts, len(result.Attestations))
		}

		// All queries should return the same number of results
		for i := 1; i < len(resultCounts); i++ {
			assert.Equal(t, resultCounts[0], resultCounts[i],
				"All case variations should return the same number of results")
		}

		t.Logf("✓ All case variations returned consistent results: %v", resultCounts)
	})
}
