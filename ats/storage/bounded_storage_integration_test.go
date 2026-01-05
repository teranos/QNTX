package storage

import (
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage/testutil"
	"github.com/teranos/QNTX/ats/types"
)

// TestBoundedStorage_16PerActorContext verifies that only 16 attestations
// are kept per actor/context pair
func TestBoundedStorage_16PerActorContext(t *testing.T) {
	db := testutil.SetupTestDB(t)

	store := NewBoundedStore(db, nil)
	actor := "test@bounded-storage"
	subject := "KYSTSN"

	// Insert 20 attestations with the same actor but different predicates/contexts
	// This simulates data where one entity has many different attributes
	for i := 0; i < 20; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("TEST_%d", i),
			Subjects:   []string{subject},
			Predicates: []string{fmt.Sprintf("predicate_%d", i)},
			Contexts:   []string{fmt.Sprintf("context_%d", i)},
			Actors:     []string{actor},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "test",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(attestation)
		require.NoError(t, err, "Failed to create attestation %d", i)
	}

	// Count total attestations - should NOT be limited since each has unique context
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations WHERE json_extract(subjects, '$[0]') = ?", subject).Scan(&count)
	require.NoError(t, err)

	assert.Equal(t, 20, count, "All 20 attestations should exist (different contexts)")

	t.Logf("✓ Test passed: Different contexts = no pruning (20 attestations exist)")
}

// TestBoundedStorage_SameActorContextPruning verifies that when we exceed 16 attestations
// for the SAME actor AND SAME context, oldest ones are pruned
func TestBoundedStorage_SameActorContextPruning(t *testing.T) {
	db := testutil.SetupTestDB(t)

	store := NewBoundedStore(db, nil)
	actor := "test@bounded-storage"
	subject := "KYSTSN"
	context := "10.0" // Same context for all

	// Insert 20 attestations with the same actor AND same context
	// This triggers the 16-per-actor/context limit
	for i := 0; i < 20; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("TEST_SAME_CTX_%d", i),
			Subjects:   []string{subject},
			Predicates: []string{fmt.Sprintf("predicate_%d", i)},
			Contexts:   []string{context}, // SAME context
			Actors:     []string{actor},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "test",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(attestation)
		require.NoError(t, err, "Failed to create attestation %d", i)
	}

	// Count total attestations - should be limited to 16
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations WHERE json_extract(subjects, '$[0]') = ?", subject).Scan(&count)
	require.NoError(t, err)

	assert.LessOrEqual(t, count, 16, "Should be limited to 16 attestations per actor/context")

	t.Logf("✓ Test result: %d attestations exist (limit: 16 per actor/context)", count)
}

// TestBoundedStorage_DomainScenario simulates a realistic domain scenario
// where multiple predicates exist for one subject with the same actor
func TestBoundedStorage_DomainScenario(t *testing.T) {
	db := testutil.SetupTestDB(t)

	store := NewBoundedStore(db, nil)
	actor := "test@domain-integration"
	subject := "KYSTSN"

	// Simulate domain attestations for entity
	domainAttestations := []struct {
		pred string
		ctx  string
	}{
		{"duration_in_role_a", "0.0"},
		{"duration_in_role_b", "10.0"},
		{"total_duration", "10.0"}, // CRITICAL for OVER query
		{"level", "level_3"},
		{"has_sufficient_duration", "true"},
		{"has_attribute", "attribute_senior"},
		{"category", "category_b"},
		{"type", "type_1"},
		{"type", "type_2"},
		{"rank", "senior"},
		{"language", "lang_1"},
		{"language", "lang_2"},
		{"capability", "cap_a"},
		{"capability", "cap_b"},
		{"capability", "cap_c"},
		{"capability", "cap_d"},
		{"capability", "cap_e"},
		{"capability", "cap_f"},
		{"credential", "credential_1"},
		{"background", "background_a"},
	}

	for i, att := range domainAttestations {
		attestation := &types.As{
			ID:         fmt.Sprintf("KYSTSN_DOMAIN_%d", i),
			Subjects:   []string{subject},
			Predicates: []string{att.pred},
			Contexts:   []string{att.ctx},
			Actors:     []string{actor},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "domain_test",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(attestation)
		require.NoError(t, err, "Failed to create domain attestation: %s", att.pred)
	}

	// Count total attestations
	var totalCount int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations WHERE json_extract(subjects, '$[0]') = ?", subject).Scan(&totalCount)
	require.NoError(t, err)

	t.Logf("Total attestations for %s: %d (inserted: %d)", subject, totalCount, len(domainAttestations))

	// Check if the CRITICAL attestation exists
	var criticalExists bool
	err = db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM attestations
			WHERE json_extract(subjects, '$[0]') = ?
			AND json_extract(predicates, '$[0]') = 'total_duration'
			AND json_extract(contexts, '$[0]') = '10.0'
		)
	`, subject).Scan(&criticalExists)
	require.NoError(t, err)

	assert.True(t, criticalExists, "Critical attestation (total_duration=10.0) should exist")

	// Show all duration-related attestations
	rows, err := db.Query(`
		SELECT json_extract(predicates, '$[0]'), json_extract(contexts, '$[0]')
		FROM attestations
		WHERE json_extract(subjects, '$[0]') = ?
		AND json_extract(predicates, '$[0]') LIKE '%duration%'
		ORDER BY json_extract(predicates, '$[0]')
	`, subject)
	require.NoError(t, err)
	defer rows.Close()

	t.Log("Duration attestations:")
	durationCount := 0
	for rows.Next() {
		var pred, ctx string
		rows.Scan(&pred, &ctx)
		t.Logf("  %s: %s", pred, ctx)
		durationCount++
	}

	assert.Greater(t, durationCount, 0, "Should have duration attestations")

	if !criticalExists {
		t.Error("BOUNDED STORAGE IS PRUNING THE CRITICAL ATTESTATION!")
	}
}
