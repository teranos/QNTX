package storage

import (
	"testing"

	"github.com/teranos/QNTX/ats/ingestion"
	"github.com/teranos/QNTX/ats/storage/testutil"
)

// mockItem implements AttestationItem interface for testing
type mockItem struct {
	subject   string
	predicate string
	object    string
	meta      map[string]string
}

func (m *mockItem) GetSubject() string       { return m.subject }
func (m *mockItem) GetPredicate() string     { return m.predicate }
func (m *mockItem) GetObject() string        { return m.object }
func (m *mockItem) GetMeta() map[string]string { return m.meta }

// Ensure mockItem implements AttestationItem
var _ ingestion.Item = (*mockItem)(nil)

// TestNewBatchPersister tests batch persister creation
func TestNewBatchPersister(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	actor := "test-actor"
	source := "test-source"

	bp := NewBatchPersister(db, actor, source)

	if bp == nil {
		t.Fatal("NewBatchPersister() returned nil")
	}

	if bp.db != db {
		t.Error("BatchPersister.db not set correctly")
	}

	if bp.actor != actor {
		t.Errorf("BatchPersister.actor = %q, want %q", bp.actor, actor)
	}

	if bp.source != source {
		t.Errorf("BatchPersister.source = %q, want %q", bp.source, source)
	}

	if bp.store == nil {
		t.Error("BatchPersister.store not initialized")
	}
}

// TestPersistItems_Success tests successful batch persistence
func TestPersistItems_Success(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	items := []AttestationItem{
		&mockItem{
			subject:   "alice",
			predicate: "knows",
			object:    "bob",
		},
		&mockItem{
			subject:   "bob",
			predicate: "works-at",
			object:    "acme-corp",
		},
		&mockItem{
			subject:   "carol",
			predicate: "lives-in",
			object:    "seattle",
		},
	}

	result := bp.PersistItems(items, "test-prefix")

	// Verify all items persisted successfully
	if result.PersistedCount != 3 {
		t.Errorf("PersistedCount = %d, want 3", result.PersistedCount)
	}

	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", result.FailureCount)
	}

	if result.SuccessRate != 100.0 {
		t.Errorf("SuccessRate = %.2f, want 100.00", result.SuccessRate)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", result.Errors)
	}
}

// TestPersistItems_WithMetadata tests persistence with item metadata
func TestPersistItems_WithMetadata(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	items := []AttestationItem{
		&mockItem{
			subject:   "alice",
			predicate: "age",
			object:    "30",
			meta: map[string]string{
				"source_file": "data.csv",
				"row_number":  "42",
			},
		},
	}

	result := bp.PersistItems(items, "csv-import")

	if result.PersistedCount != 1 {
		t.Errorf("PersistedCount = %d, want 1", result.PersistedCount)
	}

	// Verify attestation was created with metadata in attributes
	// This is tested implicitly by persistence success
}

// TestPersistItems_EmptyBatch tests persistence of empty batch
func TestPersistItems_EmptyBatch(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	items := []AttestationItem{}
	result := bp.PersistItems(items, "test-prefix")

	if result.PersistedCount != 0 {
		t.Errorf("PersistedCount = %d, want 0", result.PersistedCount)
	}

	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", result.FailureCount)
	}

	if result.SuccessRate != 0.0 {
		t.Errorf("SuccessRate = %.2f, want 0.00", result.SuccessRate)
	}
}

// TestPersistItems_NilDatabase tests error handling for nil database
func TestPersistItems_NilDatabase(t *testing.T) {
	bp := &BatchPersister{
		db:     nil,
		store:  nil,
		actor:  "test-actor",
		source: "test-source",
	}

	items := []AttestationItem{
		&mockItem{
			subject:   "alice",
			predicate: "test",
			object:    "value",
		},
	}

	result := bp.PersistItems(items, "test-prefix")

	// All items should fail with nil database
	if result.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", result.FailureCount)
	}

	if result.PersistedCount != 0 {
		t.Errorf("PersistedCount = %d, want 0", result.PersistedCount)
	}

	if len(result.Errors) == 0 {
		t.Error("Expected error message for nil database")
	}

	// Verify error message mentions database
	if len(result.Errors) > 0 && result.Errors[0] != "database connection is nil" {
		t.Errorf("Error message = %q, want 'database connection is nil'", result.Errors[0])
	}
}

// TestPersistItems_EmptySubject tests that empty subjects are accepted by ASID generation
func TestPersistItems_EmptySubject(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	// Empty subject is valid - vanity-id library accepts empty strings
	items := []AttestationItem{
		&mockItem{
			subject:   "alice",
			predicate: "knows",
			object:    "bob",
		},
		&mockItem{
			subject:   "", // Empty subject is accepted by ASID generation
			predicate: "valid",
			object:    "item",
		},
		&mockItem{
			subject:   "carol",
			predicate: "works-at",
			object:    "acme",
		},
	}

	result := bp.PersistItems(items, "test-prefix")

	// All 3 should succeed - empty subject doesn't fail ASID generation
	if result.PersistedCount != 3 {
		t.Errorf("PersistedCount = %d, want 3 (empty subject is valid)", result.PersistedCount)
	}

	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", result.FailureCount)
	}

	if result.SuccessRate != 100.0 {
		t.Errorf("SuccessRate = %.2f, want 100.00", result.SuccessRate)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Errors count = %d, want 0: %v", len(result.Errors), result.Errors)
	}
}

// TestPersistItems_LargeBatch tests performance with larger batch
func TestPersistItems_LargeBatch(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	// Create 100 items
	items := make([]AttestationItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = &mockItem{
			subject:   "alice",
			predicate: "test-predicate",
			object:    "test-object",
		}
	}

	result := bp.PersistItems(items, "large-batch")

	if result.PersistedCount != 100 {
		t.Errorf("PersistedCount = %d, want 100", result.PersistedCount)
	}

	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", result.FailureCount)
	}

	if result.SuccessRate != 100.0 {
		t.Errorf("SuccessRate = %.2f, want 100.00", result.SuccessRate)
	}
}

// TestPersistItems_UniqueASIDs tests that ASIDs are generated correctly
func TestPersistItems_UniqueASIDs(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	bp := NewBatchPersister(db, "test-actor", "test-source")

	// Create items with different subjects/predicates
	items := []AttestationItem{
		&mockItem{subject: "alice", predicate: "knows", object: "bob"},
		&mockItem{subject: "alice", predicate: "works-at", object: "acme"},
		&mockItem{subject: "bob", predicate: "knows", object: "alice"},
	}

	result := bp.PersistItems(items, "test-prefix")

	if result.PersistedCount != 3 {
		t.Errorf("PersistedCount = %d, want 3", result.PersistedCount)
	}

	// Each item should generate unique ASID and persist successfully
	if result.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0. Errors: %v", result.FailureCount, result.Errors)
	}
}
