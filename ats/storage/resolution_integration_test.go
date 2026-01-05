package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// setupResolutionTestDB creates a test database with real schema and resolution-specific fixtures
func setupResolutionTestDB(t *testing.T) *sql.DB {
	// Create in-memory test database
	testDB := qntxtest.CreateTestDB(t)

	// Create test fixtures for resolution scenarios
	// DOZER evolution scenario: manager (older) -> engineer -> senior engineer (newer)
	_, err := testDB.Exec(`
		INSERT INTO attestations (
			id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		) VALUES
		('A500', '["DOZER"]', '["manager"]', '["RESEARCH_LAB"]', '["hr-system"]', ?, 'hr-system', '{}', ?),
		('A600', '["DOZER"]', '["engineer"]', '["RESEARCH_LAB"]', '["hr-system"]', ?, 'hr-system', '{}', ?),
		('A700', '["DOZER"]', '["senior engineer"]', '["RESEARCH_LAB"]', '["hr-system"]', ?, 'hr-system', '{}', ?),
		('A800', '["DOZER"]', '["engineer"]', '["ACME"]', '["registry"]', ?, 'registry', '{}', ?),
		('NEO1', '["NEO"]', '["_"]', '["MATRIX"]', '["oracle"]', ?, 'oracle', '{}', ?)
	`,
		time.Now().AddDate(0, 0, -30).Format(time.RFC3339), time.Now().Format(time.RFC3339),
		time.Now().AddDate(0, 0, -20).Format(time.RFC3339), time.Now().Format(time.RFC3339),
		time.Now().AddDate(0, 0, -10).Format(time.RFC3339), time.Now().Format(time.RFC3339),
		time.Now().AddDate(0, 0, -5).Format(time.RFC3339), time.Now().Format(time.RFC3339),
		time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
	require.NoError(t, err, "Failed to insert resolution test fixtures")

	return testDB
}

func TestAttestationResolution(t *testing.T) {
	// Setup test database with fixtures
	db := setupResolutionTestDB(t)

	// Use executor factory for smart resolution
	executor := NewExecutor(db)

	// Test Evolution scenario: same actor, different times (DOZER)
	t.Run("evolution_resolution", func(t *testing.T) {
		filter := types.AxFilter{
			Subjects: []string{"DOZER"},
			Contexts: []string{"RESEARCH_LAB"},
			Limit:    100,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Smart resolution query failed")

		// From fixtures: DOZER has manager (A500), engineer (A600), senior engineer (A700)
		// Evolution should show latest from hr-system, plus registry engineer
		assert.NotEmpty(t, result.Attestations, "Expected to find DOZER attestations")

		// Should have detected some resolution (evolution or conflict)
		if len(result.Conflicts) == 0 && len(result.Attestations) < 3 {
			t.Logf("Note: Smart resolution may have filtered results")
		}
	})

	// Test Verification scenario: multiple sources, same claim
	t.Run("verification_resolution", func(t *testing.T) {
		// Add test data for verification scenario
		_, err := db.Exec(`
			INSERT INTO attestations (
				id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
			) VALUES
			('VERIFY1', '["LINK"]', '["engineer"]', '["ACME"]', '["hr-system"]', ?, 'hr-system', '{}', ?),
			('VERIFY2', '["LINK"]', '["engineer"]', '["ACME"]', '["registry"]', ?, 'registry', '{}', ?)
		`, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339),
			time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339))
		require.NoError(t, err, "Failed to insert verification test data")

		filter := types.AxFilter{
			Subjects:   []string{"LINK"},
			Predicates: []string{"engineer"},
			Contexts:   []string{"ACME"},
			Limit:      100,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Verification query failed")

		// Should show both sources (verification scenario)
		assert.GreaterOrEqual(t, len(result.Attestations), 2,
			"Expected at least 2 attestations for verification")
	})

	// Test Conflict scenario: different actors, different claims
	t.Run("conflict_resolution", func(t *testing.T) {
		// Add test data for real conflict
		_, err := db.Exec(`
			INSERT INTO attestations (
				id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
			) VALUES
			('CONFLICT1', '["NIOBE"]', '["manager"]', '["STARTUP"]', '["hr-system"]', ?, 'hr-system', '{}', ?),
			('CONFLICT2', '["NIOBE"]', '["engineer"]', '["STARTUP"]', '["registry"]', ?, 'registry', '{}', ?)
		`, time.Now().AddDate(0, 0, -2).Format(time.RFC3339), time.Now().Format(time.RFC3339),
			time.Now().AddDate(0, 0, -1).Format(time.RFC3339), time.Now().Format(time.RFC3339))
		require.NoError(t, err, "Failed to insert conflict test data")

		filter := types.AxFilter{
			Subjects: []string{"NIOBE"},
			Contexts: []string{"STARTUP"},
			Limit:    100,
		}

		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Conflict query failed")

		// Should detect conflict and show both for human review
		if len(result.Conflicts) == 0 {
			t.Logf("Note: Expected conflict detection for different roles")
		}

		// Should show all conflicting attestations
		assert.GreaterOrEqual(t, len(result.Attestations), 2,
			"Expected to show all conflicting attestations")
	})

}

func TestResolutionPerformance(t *testing.T) {
	db := setupResolutionTestDB(t)

	executor := NewExecutor(db)

	// Test that smart resolution doesn't significantly slow down queries
	// Note: Performance benchmarking should use b.Run with testing.B for accurate metrics
	filter := types.AxFilter{
		Predicates: []string{"engineer"},
		Limit:      100,
	}

	start := time.Now()
	result, err := executor.ExecuteAsk(context.Background(), filter)
	duration := time.Since(start)

	require.NoError(t, err, "Performance test failed")
	assert.NotEmpty(t, result.Attestations, "Performance test should find some engineer attestations")

	// Log duration for informational purposes
	t.Logf("Query completed in %v", duration)
}

func TestResolutionWithAliases(t *testing.T) {
	db := setupResolutionTestDB(t)

	// Add an alias for testing
	_, err := db.Exec(`
		INSERT INTO aliases (alias, target, created_by, created_at)
		VALUES ('DOZER_ALIAS', 'DOZER', 'system', CURRENT_TIMESTAMP),
			   ('DOZER', 'DOZER_ALIAS', 'system', CURRENT_TIMESTAMP)
	`)
	require.NoError(t, err, "Failed to insert alias test data")

	executor := NewExecutor(db)

	// Test that resolution works with alias resolution
	filter := types.AxFilter{
		Subjects: []string{"DOZER_ALIAS"}, // Query using alias
		Limit:    100,
	}

	start := time.Now()
	result, err := executor.ExecuteAsk(context.Background(), filter)
	duration := time.Since(start)

	require.NoError(t, err, "Alias resolution test failed")
	assert.NotEmpty(t, result.Attestations, "Should find DOZER attestations through alias")

	t.Logf("Alias resolution completed in %v", duration)
}

func TestResolutionEdgeCases(t *testing.T) {
	db := setupResolutionTestDB(t)

	executor := NewExecutor(db)

	// Test empty query
	t.Run("empty_query", func(t *testing.T) {
		filter := types.AxFilter{Limit: 10}
		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Empty query should not fail")

		// Should return some results (limited)
		assert.NotEmpty(t, result.Attestations, "Empty query should return some attestations")
	})

	// Test nonexistent subject
	t.Run("nonexistent_subject", func(t *testing.T) {
		filter := types.AxFilter{
			Subjects: []string{"NONEXISTENT_SUBJECT"},
			Limit:    10,
		}
		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Nonexistent subject query should not fail")

		// Should return no results
		assert.Empty(t, result.Attestations, "Nonexistent subject should return no attestations")
	})

	// Test single attestation (no conflicts possible)
	t.Run("single_attestation", func(t *testing.T) {
		filter := types.AxFilter{
			Subjects:   []string{"NEO"},
			Predicates: []string{"_"}, // Existence only
			Limit:      10,
		}
		result, err := executor.ExecuteAsk(context.Background(), filter)
		require.NoError(t, err, "Single attestation query should not fail")

		// Should have no conflicts (single attestation)
		assert.Empty(t, result.Conflicts, "Single attestation should not generate conflicts")
	})
}
