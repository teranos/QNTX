package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage/testutil"
	"github.com/teranos/QNTX/ats/types"
)

// TestBoundedStorage_DoesNotDeleteDifferentContexts validates that attestations
// with DIFFERENT contexts are NOT deleted when enforcing actor/context limits
//
// 📚 The Library of Alexandria: Different Subjects
// A librarian catalogs 20 different scrolls about 20 different subjects.
// Each subject gets its own shelf (context), so no scrolls are discarded.
func TestBoundedStorage_DoesNotDeleteDifferentContexts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewBoundedStore(db, nil)
	librarian := "hypatia@alexandria"

	// Create 20 attestations with DIFFERENT contexts (different subjects = different shelves)
	subjects := []string{"Mathematics", "Astronomy", "Medicine", "Philosophy", "Geography",
		"History", "Poetry", "Drama", "Music", "Architecture",
		"Engineering", "Biology", "Chemistry", "Physics", "Logic",
		"Rhetoric", "Grammar", "Ethics", "Politics", "Economics"}

	for i, subject := range subjects {
		attestation := &types.As{
			ID:         fmt.Sprintf("SCROLL_%d", i+1),
			Subjects:   []string{"Library"},
			Predicates: []string{"contains_knowledge"},
			Contexts:   []string{subject}, // Each subject is a unique context
			Actors:     []string{librarian},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "alexandria",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err, "Failed to catalog %s scroll", subject)
	}

	// All 20 should exist (different contexts = different storage limits)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count)
	require.NoError(t, err)

	assert.Equal(t, 20, count, "Library should preserve all scrolls about different subjects")

	// Verify none were discarded
	for i := 1; i <= 20; i++ {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)", fmt.Sprintf("SCROLL_%d", i)).Scan(&exists)
		assert.True(t, exists, "Scroll %d should exist in library", i)
	}
}

// TestBoundedStorage_DeletesWhenExceeding16PerActorContext validates that
// when we exceed 16 attestations for the SAME (actor, context) pair,
// the OLDEST ones are deleted
//
// 📚 The Library of Alexandria: Shelf Space Limits
// A librarian tries to shelve 20 astronomy scrolls, but the astronomy shelf
// only has room for 16. The oldest 4 scrolls are moved to archives (deleted).
func TestBoundedStorage_DeletesWhenExceeding16PerActorContext(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewBoundedStore(db, nil)
	librarian := "ptolemy@alexandria"
	subject := "Astronomy" // All scrolls about the same subject

	// Create 20 astronomy scrolls - but the shelf only holds 16!
	for i := 1; i <= 20; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("ASTRO_SCROLL_%d", i),
			Subjects:   []string{"Library"},
			Predicates: []string{fmt.Sprintf("observes_constellation_%d", i)},
			Contexts:   []string{subject}, // All same subject = same shelf
			Actors:     []string{librarian},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "alexandria",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err, "Failed to catalog astronomy scroll %d", i)
	}

	// Only 16 should exist (shelf capacity limit enforced)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count)
	require.NoError(t, err)

	assert.Equal(t, 16, count, "Astronomy shelf should hold exactly 16 scrolls (capacity limit)")

	// First 4 scrolls should be archived (oldest)
	for i := 1; i <= 4; i++ {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)", fmt.Sprintf("ASTRO_SCROLL_%d", i)).Scan(&exists)
		assert.False(t, exists, "Oldest astronomy scroll %d should be archived", i)
	}

	// Last 16 scrolls should remain on shelf (newest)
	for i := 5; i <= 20; i++ {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)", fmt.Sprintf("ASTRO_SCROLL_%d", i)).Scan(&exists)
		assert.True(t, exists, "Recent astronomy scroll %d should remain on shelf", i)
	}
}

// TestBoundedStorage_DoesNotDeleteCrossingContextBoundaries validates that
// enforcing limits for one context does NOT affect attestations with different contexts
//
// 📚 The Library of Alexandria: Independent Shelves
// A librarian manages two overfull shelves: Philosophy (17 scrolls) and Medicine (17 scrolls).
// Each shelf independently archives its oldest scroll. Philosophy doesn't affect Medicine.
func TestBoundedStorage_DoesNotDeleteCrossingContextBoundaries(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewBoundedStore(db, nil)
	librarian := "eratosthenes@alexandria"

	// Shelve 17 Philosophy scrolls (shelf holds 16)
	for i := 1; i <= 17; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("PHIL_SCROLL_%d", i),
			Subjects:   []string{"Library"},
			Predicates: []string{fmt.Sprintf("explores_philosophy_topic_%d", i)},
			Contexts:   []string{"Philosophy"},
			Actors:     []string{librarian},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "alexandria",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err)
	}

	// Shelve 17 Medicine scrolls (shelf holds 16)
	for i := 1; i <= 17; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("MED_SCROLL_%d", i),
			Subjects:   []string{"Library"},
			Predicates: []string{fmt.Sprintf("describes_treatment_%d", i)},
			Contexts:   []string{"Medicine"},
			Actors:     []string{librarian},
			Timestamp:  time.Now().Add(time.Duration(i+100) * time.Second),
			Source:     "alexandria",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err)
	}

	// Should have 16 from Philosophy + 16 from Medicine = 32 total
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count)
	require.NoError(t, err)

	assert.Equal(t, 32, count, "Library should have 16 scrolls per subject (independent shelf limits)")

	// Verify both shelves archived their oldest scroll independently
	var philOldest, medOldest bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = 'PHIL_SCROLL_1')").Scan(&philOldest)
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = 'MED_SCROLL_1')").Scan(&medOldest)

	assert.False(t, philOldest, "Oldest Philosophy scroll should be archived")
	assert.False(t, medOldest, "Oldest Medicine scroll should be archived (limits don't cross shelves)")
}

// TestBoundedStorage_MixedContextsPreservation validates a realistic domain scenario:
// Mix of empty contexts and valued contexts, ensuring valued contexts are preserved
func TestBoundedStorage_MixedContextsPreservation(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewBoundedStore(db, nil)
	actor := "test@domain-integration"

	// Simulate domain pattern: First create many empty-context attestations
	for i := 1; i <= 17; i++ {
		attestation := &types.As{
			ID:         fmt.Sprintf("EMPTY_%d", i),
			Subjects:   []string{"ENTITY"},
			Predicates: []string{fmt.Sprintf("semantic_pred_%d", i)},
			Contexts:   []string{""}, // Empty context
			Actors:     []string{actor},
			Timestamp:  time.Now().Add(time.Duration(i) * time.Second),
			Source:     "test",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err)
	}

	// Now create critical attestations with valued contexts
	criticalAttestations := []struct {
		id   string
		pred string
		ctx  string
	}{
		{"CRITICAL_1", "total_years_experience", "10.0"},
		{"CRITICAL_2", "years_in_specialty", "8.0"},
		{"CRITICAL_3", "proficiency_score", "0.0"},
		{"CRITICAL_4", "expertise_level", "advanced"},
	}

	for _, att := range criticalAttestations {
		attestation := &types.As{
			ID:         att.id,
			Subjects:   []string{"ENTITY"},
			Predicates: []string{att.pred},
			Contexts:   []string{att.ctx}, // Valued context
			Actors:     []string{actor},
			Timestamp:  time.Now().Add(time.Duration(100) * time.Second),
			Source:     "test",
			CreatedAt:  time.Now(),
		}

		err := store.CreateAttestation(context.Background(), attestation)
		require.NoError(t, err)
	}

	// Verify ALL critical attestations still exist
	for _, att := range criticalAttestations {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)", att.id).Scan(&exists)
		assert.True(t, exists, "Critical attestation %s (%s=%s) should be preserved", att.id, att.pred, att.ctx)
	}

	// Verify empty-context attestations are limited to 16
	var emptyCount int
	db.QueryRow(`SELECT COUNT(*) FROM attestations WHERE json_extract(contexts, '$[0]') = ''`).Scan(&emptyCount)
	assert.LessOrEqual(t, emptyCount, 16, "Empty-context attestations should be limited to 16")

	// Verify the oldest empty-context attestation was deleted
	var empty1Exists bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = 'EMPTY_1')").Scan(&empty1Exists)
	assert.False(t, empty1Exists, "Oldest empty-context attestation should be deleted")
}

// TestBoundedStorage_ExactDomainReproduction reproduces a realistic E2E test scenario
// with the order and types of attestations that a typical domain generates
func TestBoundedStorage_ExactDomainReproduction(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewBoundedStore(db, nil)
	actor := "test@domain-integration"

	// Simulate processing 9 entities, each generating:
	// - 3 empty-context attestations (human, entity, data_entity)
	// - 4 duration attestations (total_duration, duration_role_a, duration_role_b, level)
	// - Various other attestations

	entityID := 0
	for entity := 1; entity <= 9; entity++ {
		// Empty-context attestations (these accumulate and trigger deletions)
		for _, pred := range []string{"human", "entity", "data_entity"} {
			entityID++
			attestation := &types.As{
				ID:         fmt.Sprintf("TEST_%d", entityID),
				Subjects:   []string{fmt.Sprintf("ENT_%d", entity)},
				Predicates: []string{pred},
				Contexts:   []string{""}, // Empty
				Actors:     []string{actor},
				Timestamp:  time.Now().Add(time.Duration(entityID) * time.Second),
				Source:     "test",
				CreatedAt:  time.Now(),
			}
			err := store.CreateAttestation(context.Background(), attestation)
			require.NoError(t, err)
		}

		// Duration attestations (these should be preserved)
		durationAtts := []struct {
			pred string
			ctx  string
		}{
			{"total_duration", fmt.Sprintf("%.1f", float64(entity)*2)},
			{"duration_in_role_a", fmt.Sprintf("%.1f", float64(entity))},
			{"duration_in_role_b", "0.0"},
			{"level", "level_3"},
		}

		for _, dur := range durationAtts {
			entityID++
			attestation := &types.As{
				ID:         fmt.Sprintf("TEST_%d", entityID),
				Subjects:   []string{fmt.Sprintf("ENT_%d", entity)},
				Predicates: []string{dur.pred},
				Contexts:   []string{dur.ctx}, // Valued context
				Actors:     []string{actor},
				Timestamp:  time.Now().Add(time.Duration(entityID) * time.Second),
				Source:     "test",
				CreatedAt:  time.Now(),
			}
			err := store.CreateAttestation(context.Background(), attestation)
			require.NoError(t, err)
		}
	}

	// Verify ALL duration attestations for ALL entities still exist
	for entity := 1; entity <= 9; entity++ {
		var totalDurationExists bool
		db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM attestations
				WHERE json_extract(subjects, '$[0]') = ?
				AND json_extract(predicates, '$[0]') = 'total_duration'
			)
		`, fmt.Sprintf("ENT_%d", entity)).Scan(&totalDurationExists)

		assert.True(t, totalDurationExists,
			"Entity %d should have total_duration attestation", entity)
	}

	// Verify empty-context attestations are limited
	var emptyCount int
	db.QueryRow(`SELECT COUNT(*) FROM attestations WHERE json_extract(contexts, '$[0]') = ''`).Scan(&emptyCount)
	assert.LessOrEqual(t, emptyCount, 16, "Empty-context attestations should be limited to 16")
}
