//go:build rustsqlite
// +build rustsqlite

package sqlitecgo

import (
	"fmt"
	"os"
	"path/filepath"
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

// A closed store says so, rather than handing its freed store to Rust, and a
// second Close frees nothing twice.
func TestRustStore_ClosedSaysSo(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}

	if _, err := store.GetAttestation("AS-gone"); err == nil || err.Error() != "store is closed" {
		t.Errorf("GetAttestation on a closed store: %v", err)
	}
	if _, err := store.CountAttestations(); err == nil || err.Error() != "store is closed" {
		t.Errorf("CountAttestations on a closed store: %v", err)
	}
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
		Attributes: make(map[string]any),
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
		Subjects:   []string{"CHARLIE"},
		Predicates: []string{"maintains"},
		Contexts:   []string{"ats-sqlite"},
		Actors:     []string{"human:alice"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: make(map[string]any),
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

	// Create two attestations with varied data
	attestations := []*types.As{
		{
			ID:         "AS-count-1",
			Subjects:   []string{"DOC-123"},
			Predicates: []string{"reviewed_by"},
			Contexts:   []string{"Pull Request #333"},
			Actors:     []string{"human:sebastian"},
			Timestamp:  time.Now(),
			Source:     "github",
			Attributes: make(map[string]any),
		},
		{
			ID:         "AS-count-2",
			Subjects:   []string{"ats"},
			Predicates: []string{"built_by"},
			Contexts:   []string{"CI"},
			Actors:     []string{"bot:github-actions"},
			Timestamp:  time.Now(),
			Source:     "ci",
			Attributes: make(map[string]any),
		},
	}

	for _, as := range attestations {
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

	// Create attestations with varied domains
	attestations := []*types.As{
		{
			ID:         "AS-1",
			Subjects:   []string{"Carbonara"},
			Predicates: []string{"prepared_by"},
			Contexts:   []string{"Italian cuisine"},
			Actors:     []string{"human:chef_mario"},
			Timestamp:  time.Now(),
			Source:     "cooking_app",
			Attributes: make(map[string]any),
		},
		{
			ID:         "AS-2",
			Subjects:   []string{"Symphony_No_9"},
			Predicates: []string{"composed_by"},
			Contexts:   []string{"Classical"},
			Actors:     []string{"human:beethoven"},
			Timestamp:  time.Now(),
			Source:     "music_db",
			Attributes: make(map[string]any),
		},
		{
			ID:         "AS-3",
			Subjects:   []string{"San_Francisco"},
			Predicates: []string{"temperature"},
			Contexts:   []string{"2026-01-26"},
			Actors:     []string{"sensor:weather_station"},
			Timestamp:  time.Now(),
			Source:     "weather_api",
			Attributes: make(map[string]any),
		},
	}

	ids := []string{"AS-1", "AS-2", "AS-3"}
	for _, as := range attestations {
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation(%s) error: %v", as.ID, err)
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

func TestRustStore_Update(t *testing.T) {
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("NewMemoryStore() error: %v", err)
	}
	defer store.Close()

	as := &types.As{
		ID:         "AS-update-1",
		Subjects:   []string{"EEG_Alpha_Wave"},
		Predicates: []string{"detected_in"},
		Contexts:   []string{"Patient_123"},
		Actors:     []string{"device:neuralink_sensor"},
		Timestamp:  time.Now(),
		Source:     "neurotech",
		Attributes: make(map[string]any),
	}

	// Create
	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() error: %v", err)
	}

	// Update to Beta wave
	as.Subjects = []string{"EEG_Beta_Wave"}
	if err := store.UpdateAttestation(as); err != nil {
		t.Fatalf("UpdateAttestation() error: %v", err)
	}

	// Verify update
	retrieved, err := store.GetAttestation("AS-update-1")
	if err != nil {
		t.Fatalf("GetAttestation() error: %v", err)
	}
	if len(retrieved.Subjects) != 1 || retrieved.Subjects[0] != "EEG_Beta_Wave" {
		t.Errorf("Subjects after update = %v, want [EEG_Beta_Wave]", retrieved.Subjects)
	}
}

// PASSIVE copies the WAL and leaves the file its size; TRUNCATE gives the file
// back, and is asked for only once the WAL has reached truncateAt.
func TestRustStore_WALCheckpoint_PassiveUntilTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "landing.db")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error: %v", err)
	}
	defer store.Close()

	for i := 0; i < 20; i++ {
		if err := store.CreateAttestation(&types.As{
			ID: fmt.Sprintf("AS-wal-%d", i), Subjects: []string{"s"}, Predicates: []string{"p"},
			Contexts: []string{"c"}, Actors: []string{"a"}, Timestamp: time.Now(), Source: "test",
			Attributes: map[string]any{},
		}); err != nil {
			t.Fatalf("CreateAttestation() error: %v", err)
		}
	}
	wal := func() int64 {
		t.Helper()
		info, err := os.Stat(path + "-wal")
		if err != nil {
			t.Fatalf("the WAL could not be measured: %v", err)
		}
		return info.Size()
	}
	before := wal()
	if before == 0 {
		t.Fatal("twenty writes left an empty WAL")
	}

	busy, walPages, checkpointed, err := store.WALCheckpoint(before + 1)
	if err != nil {
		t.Fatalf("PASSIVE: %v", err)
	}
	if busy != 0 || walPages == 0 || checkpointed != walPages {
		t.Errorf("PASSIVE answered busy %d, wal pages %d, checkpointed %d", busy, walPages, checkpointed)
	}
	if got := wal(); got != before {
		t.Errorf("PASSIVE left the WAL at %d bytes, want it kept at %d", got, before)
	}

	if _, _, _, err := store.WALCheckpoint(before); err != nil {
		t.Fatalf("TRUNCATE: %v", err)
	}
	if got := wal(); got != 0 {
		t.Errorf("TRUNCATE left the WAL at %d bytes, want 0", got)
	}
}
