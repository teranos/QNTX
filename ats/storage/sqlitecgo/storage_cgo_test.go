//go:build rustsqlite
// +build rustsqlite

package sqlitecgo

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

func TestRustStore_Lifecycle(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	// Verify version
	version := Version()
	if version == "" {
		t.Error("Version() returned empty string")
	}
	t.Logf("Rust storage version: %s", version)
}

func TestRustStore_CreateAndGet(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	as := &types.As{
		ID:         "AS-test-1",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"knows"},
		Contexts:   []string{"work"},
		Actors:     []string{"human:bob"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: make(map[string]interface{}),
	}

	// Create
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Get
	retrieved, err := store.GetAttestation("AS-test-1")
	if err != nil {
		t.Fatalf("GetAttestation() error: %v", err)
	}
	if retrieved == nil {
		t.Fatal("GetAttestation() returned nil")
	}

	if retrieved.ID != "AS-test-1" {
		t.Errorf("ID = %q, want %q", retrieved.ID, "AS-test-1")
	}
	if len(retrieved.Subjects) != 1 || retrieved.Subjects[0] != "ALICE" {
		t.Errorf("Subjects = %v, want [ALICE]", retrieved.Subjects)
	}
}

func TestRustStore_Exists(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	// Check non-existent
	if store.AttestationExists("AS-nonexistent") {
		t.Error("AttestationExists() = true for nonexistent ID")
	}

	// Create
	as := &types.As{
		ID:         "AS-exists-1",
		Subjects:   []string{"TEST"},
		Predicates: []string{"test"},
		Contexts:   []string{"test"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: make(map[string]interface{}),
	}
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Check exists
	if !store.AttestationExists("AS-exists-1") {
		t.Error("AttestationExists() = false for existing ID")
	}
}

func TestRustStore_Count(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	// Initial count
	count, err := store.CountAttestations()
	if err != nil {
		t.Fatalf("CountAttestations() error: %v", err)
	}
	if count != 0 {
		t.Errorf("CountAttestations() = %d, want 0", count)
	}

	// Create two attestations
	for i := 1; i <= 2; i++ {
		as := &types.As{
			ID:         "AS-count-" + string(rune('0'+i)),
			Subjects:   []string{"TEST"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Timestamp:  time.Now(),
			Source:     "test",
			Attributes: make(map[string]interface{}),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation() error: %v", err)
		}
	}

	// Check count
	count, err = store.CountAttestations()
	if err != nil {
		t.Fatalf("CountAttestations() error: %v", err)
	}
	if count != 2 {
		t.Errorf("CountAttestations() = %d, want 2", count)
	}
}

func TestRustStore_ListIDs(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	// Create attestations
	ids := []string{"AS-1", "AS-2", "AS-3"}
	for _, id := range ids {
		as := &types.As{
			ID:         id,
			Subjects:   []string{"TEST"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Timestamp:  time.Now(),
			Source:     "test",
			Attributes: make(map[string]interface{}),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation(%s) error: %v", id, err)
		}
	}

	// List IDs
	retrieved, err := store.ListAttestationIDs()
	if err != nil {
		t.Fatalf("ListAttestationIDs() error: %v", err)
	}
	if len(retrieved) != 3 {
		t.Errorf("ListAttestationIDs() returned %d IDs, want 3", len(retrieved))
	}

	// Verify all IDs present (order may vary)
	idMap := make(map[string]bool)
	for _, id := range retrieved {
		idMap[id] = true
	}
	for _, expectedID := range ids {
		if !idMap[expectedID] {
			t.Errorf("ListAttestationIDs() missing ID %q", expectedID)
		}
	}
}

func TestRustStore_Delete(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	as := &types.As{
		ID:         "AS-delete-1",
		Subjects:   []string{"TEST"},
		Predicates: []string{"test"},
		Contexts:   []string{"test"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: make(map[string]interface{}),
	}

	// Create
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Verify exists
	if !store.AttestationExists("AS-delete-1") {
		t.Fatal("Attestation should exist before delete")
	}

	// Delete
	if err := store.DeleteAttestation("AS-delete-1"); err != nil {
		t.Fatalf("DeleteAttestation() error: %v", err)
	}

	// Verify doesn't exist
	if store.AttestationExists("AS-delete-1") {
		t.Error("Attestation should not exist after delete")
	}
}

func TestRustStore_Update(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	as := &types.As{
		ID:         "AS-update-1",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"knows"},
		Contexts:   []string{"work"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: make(map[string]interface{}),
	}

	// Create
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Update
	as.Subjects = []string{"BOB"}
	if err := store.UpdateAttestation(as); err != nil {
		t.Fatalf("UpdateAttestation() error: %v", err)
	}

	// Verify update
	retrieved, err := store.GetAttestation("AS-update-1")
	if err != nil {
		t.Fatalf("GetAttestation() error: %v", err)
	}
	if len(retrieved.Subjects) != 1 || retrieved.Subjects[0] != "BOB" {
		t.Errorf("Subjects after update = %v, want [BOB]", retrieved.Subjects)
	}
}

func TestRustStore_Clear(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	// Create attestations
	for i := 1; i <= 3; i++ {
		as := &types.As{
			ID:         "AS-clear-" + string(rune('0'+i)),
			Subjects:   []string{"TEST"},
			Predicates: []string{"test"},
			Contexts:   []string{"test"},
			Timestamp:  time.Now(),
			Source:     "test",
			Attributes: make(map[string]interface{}),
		}
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation() error: %v", err)
		}
	}

	// Verify count
	count, _ := store.CountAttestations()
	if count != 3 {
		t.Fatalf("Count before clear = %d, want 3", count)
	}

	// Clear
	if err := store.ClearAllAttestations(); err != nil {
		t.Fatalf("ClearAllAttestations() error: %v", err)
	}

	// Verify empty
	count, _ = store.CountAttestations()
	if count != 0 {
		t.Errorf("Count after clear = %d, want 0", count)
	}
}
