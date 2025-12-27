package storage

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/db"
)

func TestCheckStorageStatus_SelfCertifying(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStore(database, nil)

	// Create self-certifying attestation (actor == ID)
	as := &types.As{
		ID:         "AS123",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"active"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"AS123"}, // Self-certifying
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings for self-certifying attestation, got %d", len(warnings))
	}
}

func TestCheckStorageStatus_BelowThreshold(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	// Create 7 attestations (43.75%, below 50% threshold)
	for i := 0; i < 7; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  time.Now(),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Check status - should not warn (below 50%)
	as := &types.As{
		ID:         generateTestASID(99),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings at 43.75%% full, got %d warnings", len(warnings))
	}
}

func TestCheckStorageStatus_AtThreshold(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	// Create 8 attestations (50% exactly)
	baseTime := time.Now().Add(-25 * time.Hour) // Old enough for rate calculation
	for i := 0; i < 8; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime.Add(time.Duration(i) * time.Hour),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Check status - should warn (at 50%)
	as := &types.As{
		ID:         generateTestASID(99),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning at 50%% full, got %d", len(warnings))
	}

	w := warnings[0]
	if w.Actor != "test@user" {
		t.Errorf("expected actor 'test@user', got %s", w.Actor)
	}
	if w.Context != "PROJECT" {
		t.Errorf("expected context 'PROJECT', got %s", w.Context)
	}
	if w.Current != 8 {
		t.Errorf("expected current count 8, got %d", w.Current)
	}
	if w.Limit != 16 {
		t.Errorf("expected limit 16, got %d", w.Limit)
	}
	if w.FillPercent != 0.5 {
		t.Errorf("expected fill percent 0.5, got %f", w.FillPercent)
	}
}

func TestCheckStorageStatus_AboveCapacity(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	// Create 16 attestations (100%)
	for i := 0; i < 16; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  time.Now(),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Check status - should not warn (at capacity, enforcement handles it)
	as := &types.As{
		ID:         generateTestASID(99),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings at 100%% full (enforcement handles this), got %d", len(warnings))
	}
}

func TestCheckStorageStatus_AccelerationDetection(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	baseTime := time.Now()

	// Create 2 old attestations (week ago) - establishes baseline
	for i := 0; i < 2; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime.Add(-168 * time.Hour), // 7 days ago
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Create 6 recent attestations (last 24 hours) - creates acceleration
	for i := 2; i < 8; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime.Add(-time.Duration(24-i) * time.Hour),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Check status - should detect acceleration
	as := &types.As{
		ID:         generateTestASID(99),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}

	w := warnings[0]

	// Day rate: 6 attestations / 24 hours = 0.25/hour
	// Week rate: 8 attestations / 168 hours = 0.047/hour
	// Acceleration: 0.25 / 0.047 = ~5.3x
	if w.AccelerationFactor < 2.0 {
		t.Errorf("expected acceleration factor > 2.0 (recent burst), got %.2f", w.AccelerationFactor)
	}

	if w.RatePerHour <= 0 {
		t.Errorf("expected positive rate per hour, got %.2f", w.RatePerHour)
	}

	if w.TimeUntilFull <= 0 {
		t.Errorf("expected positive time until full, got %v", w.TimeUntilFull)
	}
}

func TestCheckStorageStatus_SlowRate(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	// Create 8 attestations very slowly (all from 1 week ago)
	baseTime := time.Now().Add(-168 * time.Hour)
	for i := 0; i < 8; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime,
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Check status - should not warn (rate too slow)
	as := &types.As{
		ID:         generateTestASID(99),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	// Rate = 0 attestations in last 24 hours / 24 = 0/hour
	// Should skip warning when rate < 0.01/hour
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for very slow rate, got %d", len(warnings))
	}
}

func TestCheckStorageStatus_MultipleContexts(t *testing.T) {
	database, err := db.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	store := NewBoundedStoreWithConfig(database, nil, &BoundedStoreConfig{
		ActorContextLimit:  16,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	})

	baseTime := time.Now().Add(-25 * time.Hour)

	// Create 8 attestations for PROJECT_A
	for i := 0; i < 8; i++ {
		as := &types.As{
			ID:         generateTestASID(i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT_A"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime.Add(time.Duration(i) * time.Hour),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Create 12 attestations for PROJECT_B
	for i := 0; i < 12; i++ {
		as := &types.As{
			ID:         generateTestASID(100 + i),
			Subjects:   []string{"ALICE"},
			Predicates: []string{"status"},
			Contexts:   []string{"PROJECT_B"},
			Actors:     []string{"test@user"},
			Timestamp:  baseTime.Add(time.Duration(i) * time.Hour),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("failed to create attestation: %v", err)
		}
	}

	// Create attestation with both contexts
	as := &types.As{
		ID:         generateTestASID(999),
		Subjects:   []string{"BOB"},
		Predicates: []string{"status"},
		Contexts:   []string{"PROJECT_A", "PROJECT_B"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
	}

	warnings := store.CheckStorageStatus(as)

	// Should get 2 warnings (one for each context)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings (one per context), got %d", len(warnings))
	}

	// Verify both contexts are represented
	contexts := make(map[string]bool)
	for _, w := range warnings {
		contexts[w.Context] = true
	}

	if !contexts["PROJECT_A"] {
		t.Error("expected warning for PROJECT_A")
	}
	if !contexts["PROJECT_B"] {
		t.Error("expected warning for PROJECT_B")
	}
}

// Helper function to generate unique test ASIDs
func generateTestASID(n int) string {
	// Simple ASID format for testing
	return "AS" + time.Now().Format("20060102150405") + string(rune('A'+n%26)) + string(rune('0'+n%10))
}
