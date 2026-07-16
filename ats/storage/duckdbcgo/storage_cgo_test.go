//go:build cgo && rustduckdb

package duckdbcgo

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// TestRoundTrip is the ADR-024 smoke test: prove that Go → CGO → Rust FFI →
// DuckDB → back holds for a single attestation. Uses a file:// location
// (no S3 credentials needed) so it can run in CI without secrets.
func TestRoundTrip(t *testing.T) {
	loc := "file://" + filepath.Join(t.TempDir(), "qntx-parquet")

	store, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("NewDuckdbStore(%q) failed: %v", loc, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() failed: %v", err)
		}
	})

	if got := store.Location(); got != loc {
		t.Errorf("Location() = %q, want %q", got, loc)
	}

	as := &types.As{
		ID:         "AS-smoketest-1",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"knows"},
		Contexts:   []string{"work"},
		Actors:     []string{"human:bob"},
		Timestamp:  time.UnixMilli(1_700_000_000_000),
		Source:     "test",
		CreatedAt:  time.UnixMilli(1_700_000_000_000),
	}

	if err := store.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() failed: %v", err)
	}

	if !store.AttestationExists(as.ID) {
		t.Errorf("AttestationExists(%q) = false after CreateAttestation, want true", as.ID)
	}

	got, err := store.GetAttestation(as.ID)
	if err != nil {
		t.Fatalf("GetAttestation(%q) failed: %v", as.ID, err)
	}
	if got == nil {
		t.Fatalf("GetAttestation(%q) = nil, want the attestation we just put", as.ID)
	}

	if got.ID != as.ID {
		t.Errorf("round-trip ID = %q, want %q", got.ID, as.ID)
	}
	if len(got.Subjects) != 1 || got.Subjects[0] != "ALICE" {
		t.Errorf("round-trip Subjects = %v, want [ALICE]", got.Subjects)
	}
	if len(got.Predicates) != 1 || got.Predicates[0] != "knows" {
		t.Errorf("round-trip Predicates = %v, want [knows]", got.Predicates)
	}
	if len(got.Actors) != 1 || got.Actors[0] != "human:bob" {
		t.Errorf("round-trip Actors = %v, want [human:bob]", got.Actors)
	}
	if got.Source != "test" {
		t.Errorf("round-trip Source = %q, want %q", got.Source, "test")
	}

	count, err := store.CountAttestations()
	if err != nil {
		t.Fatalf("CountAttestations() failed: %v", err)
	}
	if count != 1 {
		t.Errorf("CountAttestations() = %d, want 1", count)
	}
}

// TestGetMissingReturnsNil verifies the "not found" contract:
// GetAttestation returns (nil, nil), not an error.
func TestGetMissingReturnsNil(t *testing.T) {
	loc := "file://" + filepath.Join(t.TempDir(), "qntx-parquet")

	store, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("NewDuckdbStore() failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	got, err := store.GetAttestation("AS-does-not-exist")
	if err != nil {
		t.Errorf("GetAttestation of missing ID returned error %v, want nil", err)
	}
	if got != nil {
		t.Errorf("GetAttestation of missing ID returned %+v, want nil", got)
	}
}
