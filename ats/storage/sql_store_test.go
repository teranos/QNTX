package storage

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage/testutil"
	"github.com/teranos/QNTX/ats/types"
)

// TestAttestationExists_True tests existence check for existing attestation
func TestAttestationExists_True(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create test attestation
	attestation := &types.As{
		ID:         "test-asid-exists",
		Subjects:   []string{"alice"},
		Predicates: []string{"knows"},
		Contexts:   []string{"bob"},
		Actors:     []string{"test-actor"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	err := store.CreateAttestation(attestation)
	if err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Check existence
	exists := store.AttestationExists("test-asid-exists")
	if !exists {
		t.Error("AttestationExists() = false, want true for existing attestation")
	}
}

// TestAttestationExists_False tests existence check for non-existent attestation
func TestAttestationExists_False(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Check non-existent ID
	exists := store.AttestationExists("non-existent-id")
	if exists {
		t.Error("AttestationExists() = true, want false for non-existent attestation")
	}
}

// TestAttestationExists_EmptyID tests existence check with empty ID
func TestAttestationExists_EmptyID(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Check empty ID
	exists := store.AttestationExists("")
	if exists {
		t.Error("AttestationExists('') = true, want false for empty ID")
	}
}

// TestAttestationExists_MultipleChecks tests multiple existence checks
func TestAttestationExists_MultipleChecks(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create multiple attestations
	ids := []string{"id-1", "id-2", "id-3"}
	for _, id := range ids {
		attestation := &types.As{
			ID:         id,
			Subjects:   []string{"test"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}
		if err := store.CreateAttestation(attestation); err != nil {
			t.Fatalf("CreateAttestation(%s) error: %v", id, err)
		}
	}

	// Check all existing IDs
	for _, id := range ids {
		if !store.AttestationExists(id) {
			t.Errorf("AttestationExists(%q) = false, want true", id)
		}
	}

	// Check non-existent ID
	if store.AttestationExists("id-4") {
		t.Error("AttestationExists('id-4') = true, want false for non-existent ID")
	}
}

// TestAttestationExists_AfterDeletion tests existence check after deletion
func TestAttestationExists_AfterDeletion(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create attestation
	id := "to-be-deleted"
	attestation := &types.As{
		ID:         id,
		Subjects:   []string{"test"},
		Predicates: []string{"test"},
		Contexts:   []string{"test"},
		Actors:     []string{"actor"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	if err := store.CreateAttestation(attestation); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Verify it exists
	if !store.AttestationExists(id) {
		t.Fatal("Attestation should exist after creation")
	}

	// Delete it
	_, err := db.Exec("DELETE FROM attestations WHERE id = ?", id)
	if err != nil {
		t.Fatalf("Failed to delete attestation: %v", err)
	}

	// Verify it no longer exists
	if store.AttestationExists(id) {
		t.Error("AttestationExists() = true, want false after deletion")
	}
}

// TestAttestationExists_CaseSensitivity tests case sensitivity of ID matching
func TestAttestationExists_CaseSensitivity(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create attestation with specific casing
	id := "TestCaseID"
	attestation := &types.As{
		ID:         id,
		Subjects:   []string{"test"},
		Predicates: []string{"test"},
		Contexts:   []string{"test"},
		Actors:     []string{"actor"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	if err := store.CreateAttestation(attestation); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Check with exact case
	if !store.AttestationExists("TestCaseID") {
		t.Error("AttestationExists('TestCaseID') = false, want true for exact match")
	}

	// SQLite is case-sensitive for strings by default
	// Check with different case (should not exist)
	if store.AttestationExists("testcaseid") {
		t.Error("AttestationExists('testcaseid') = true, want false (case mismatch)")
	}
}

// TestAttestationExists_SpecialCharacters tests handling of special characters in IDs
func TestAttestationExists_SpecialCharacters(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	// Create attestation with special characters in ID
	specialIDs := []string{
		"id-with-dashes",
		"id_with_underscores",
		"id.with.dots",
		"id@with@at",
	}

	for _, id := range specialIDs {
		attestation := &types.As{
			ID:         id,
			Subjects:   []string{"test"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Actors:     []string{"actor"},
			Timestamp:  time.Now(),
			Source:     "test",
		}

		if err := store.CreateAttestation(attestation); err != nil {
			t.Fatalf("CreateAttestation(%q) error: %v", id, err)
		}

		// Verify existence
		if !store.AttestationExists(id) {
			t.Errorf("AttestationExists(%q) = false, want true", id)
		}
	}
}

// TestCreateAttestation_Duplicate tests duplicate prevention
func TestCreateAttestation_Duplicate(t *testing.T) {
	db := testutil.SetupTestDB(t)
	defer db.Close()

	store := NewSQLStore(db, nil)

	attestation := &types.As{
		ID:         "duplicate-test",
		Subjects:   []string{"alice"},
		Predicates: []string{"knows"},
		Contexts:   []string{"bob"},
		Actors:     []string{"actor"},
		Timestamp:  time.Now(),
		Source:     "test",
	}

	// First creation should succeed
	err := store.CreateAttestation(attestation)
	if err != nil {
		t.Fatalf("First CreateAttestation() error: %v", err)
	}

	// Verify exists
	if !store.AttestationExists("duplicate-test") {
		t.Fatal("Attestation should exist after creation")
	}

	// Second creation with same ID should fail (UNIQUE constraint)
	err = store.CreateAttestation(attestation)
	if err == nil {
		t.Error("CreateAttestation() with duplicate ID should fail, got nil error")
	}
}
