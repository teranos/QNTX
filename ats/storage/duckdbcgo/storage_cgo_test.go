//go:build cgo && rustduckdb

package duckdbcgo

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
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

// TestFlushAndReopen verifies durability across a process boundary:
// attestations put into one store, flushed to Parquet, are readable from a
// second store opened at the same location.
func TestFlushAndReopen(t *testing.T) {
	dir := t.TempDir()
	loc := "file://" + filepath.Join(dir, "qntx-parquet")

	// First lifetime: put + flush + close.
	first, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("NewDuckdbStore(%q) failed: %v", loc, err)
	}
	as := &types.As{
		ID:         "AS-flush-1",
		Subjects:   []string{"SUBJECT"},
		Predicates: []string{"predicated"},
		Contexts:   []string{"ctx"},
		Actors:     []string{"human:tester"},
		Timestamp:  time.UnixMilli(1_700_000_000_000),
		Source:     "flush-test",
		CreatedAt:  time.UnixMilli(1_700_000_000_000),
	}
	if err := first.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation() failed: %v", err)
	}
	if err := first.Flush(); err != nil {
		t.Fatalf("Flush() failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first.Close() failed: %v", err)
	}

	// Second lifetime: reopen at the same location; the flushed row must
	// be visible without any writes on the new instance.
	second, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("second NewDuckdbStore(%q) failed: %v", loc, err)
	}
	t.Cleanup(func() { second.Close() })

	got, err := second.GetAttestation(as.ID)
	if err != nil {
		t.Fatalf("second GetAttestation(%q) failed: %v", as.ID, err)
	}
	if got == nil {
		t.Fatalf("second GetAttestation(%q) = nil after flush+reopen — durability lost", as.ID)
	}
	if got.ID != as.ID {
		t.Errorf("round-trip ID = %q, want %q", got.ID, as.ID)
	}
	if len(got.Subjects) != 1 || got.Subjects[0] != "SUBJECT" {
		t.Errorf("round-trip Subjects = %v, want [SUBJECT]", got.Subjects)
	}
}

// TestGetAttestationsFilter drives the filter query end-to-end through
// Go → CGO → Rust → DuckDB → back. Fails today because DuckdbStore has
// no GetAttestations method — the call at line 47 does not compile.
//
// Once implemented, this proves the parquet backend can satisfy the
// QueryableStore interface at ats/storage/raw_store.go:31-33 and unblocks
// the "backend does not implement filter queries yet" error at
// ats/storage/ats_store.go:88.
func TestGetAttestationsFilter(t *testing.T) {
	loc := "file://" + filepath.Join(t.TempDir(), "qntx-parquet")
	store, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("NewDuckdbStore(%q) failed: %v", loc, err)
	}
	t.Cleanup(func() { store.Close() })

	// Three attestations covering the filter axes we test below.
	seed := []*types.As{
		{
			ID:         "AS-q-1",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"knows"},
			Contexts:   []string{"work"},
			Actors:     []string{"human:a"},
			Timestamp:  time.UnixMilli(1_700_000_000_000),
			Source:     "test-source-x",
			CreatedAt:  time.UnixMilli(1_700_000_000_000),
		},
		{
			ID:         "AS-q-2",
			Subjects:   []string{"BOB"},
			Predicates: []string{"knows"},
			Contexts:   []string{"work"},
			Actors:     []string{"human:b"},
			Timestamp:  time.UnixMilli(1_700_000_100_000),
			Source:     "test-source-y",
			CreatedAt:  time.UnixMilli(1_700_000_100_000),
		},
		{
			ID:         "AS-q-3",
			Subjects:   []string{"ALICE"},
			Predicates: []string{"trusts"},
			Contexts:   []string{"personal"},
			Actors:     []string{"human:a"},
			Timestamp:  time.UnixMilli(1_700_000_200_000),
			Source:     "test-source-x",
			CreatedAt:  time.UnixMilli(1_700_000_200_000),
		},
	}
	for _, as := range seed {
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation(%q) failed: %v", as.ID, err)
		}
	}

	got, err := store.GetAttestations(ats.AttestationFilter{
		Subjects: []string{"ALICE"},
	})
	if err != nil {
		t.Fatalf("filter by subject failed: %v", err)
	}
	assertIDSet(t, "subject=ALICE", got, "AS-q-1", "AS-q-3")

	got, err = store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"trusts"},
	})
	if err != nil {
		t.Fatalf("filter by predicate failed: %v", err)
	}
	assertIDSet(t, "predicate=trusts", got, "AS-q-3")

	got, err = store.GetAttestations(ats.AttestationFilter{
		Subjects:   []string{"ALICE"},
		Predicates: []string{"knows"},
	})
	if err != nil {
		t.Fatalf("filter by subject AND predicate failed: %v", err)
	}
	assertIDSet(t, "subject=ALICE AND predicate=knows", got, "AS-q-1")

	got, err = store.GetAttestations(ats.AttestationFilter{
		Contexts: []string{"personal"},
	})
	if err != nil {
		t.Fatalf("filter by context failed: %v", err)
	}
	assertIDSet(t, "context=personal", got, "AS-q-3")

	got, err = store.GetAttestations(ats.AttestationFilter{
		Actors: []string{"human:b"},
	})
	if err != nil {
		t.Fatalf("filter by actor failed: %v", err)
	}
	assertIDSet(t, "actor=human:b", got, "AS-q-2")

	got, err = store.GetAttestations(ats.AttestationFilter{
		Source: "test-source-y",
	})
	if err != nil {
		t.Fatalf("filter by source failed: %v", err)
	}
	assertIDSet(t, "source=test-source-y", got, "AS-q-2")

	tStart := time.UnixMilli(1_700_000_150_000)
	got, err = store.GetAttestations(ats.AttestationFilter{
		TimeStart: &tStart,
	})
	if err != nil {
		t.Fatalf("filter by time_start failed: %v", err)
	}
	assertIDSet(t, "time_start>=1_700_000_150_000", got, "AS-q-3")

	tEnd := time.UnixMilli(1_700_000_100_000)
	got, err = store.GetAttestations(ats.AttestationFilter{
		TimeEnd: &tEnd,
	})
	if err != nil {
		t.Fatalf("filter by time_end failed: %v", err)
	}
	assertIDSet(t, "time_end<=1_700_000_100_000", got, "AS-q-1", "AS-q-2")

	got, err = store.GetAttestations(ats.AttestationFilter{Limit: 1})
	if err != nil {
		t.Fatalf("filter with limit failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("limit=1: len(got)=%d, want 1", len(got))
	}

	got, err = store.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		t.Fatalf("empty filter failed: %v", err)
	}
	assertIDSet(t, "no filter", got, "AS-q-1", "AS-q-2", "AS-q-3")
}

// assertIDSet fails the test if the returned attestations don't have exactly
// the expected IDs (as a set — order-independent).
func assertIDSet(t *testing.T, label string, got []*types.As, want ...string) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, as := range got {
		gotSet[as.ID] = true
	}
	wantSet := make(map[string]bool, len(want))
	for _, id := range want {
		wantSet[id] = true
	}
	if len(gotSet) != len(wantSet) {
		t.Errorf("%s: got %d ids %v, want %d ids %v", label, len(gotSet), keys(gotSet), len(wantSet), want)
		return
	}
	for id := range wantSet {
		if !gotSet[id] {
			t.Errorf("%s: missing expected id %q; got %v", label, id, keys(gotSet))
		}
	}
	for id := range gotSet {
		if !wantSet[id] {
			t.Errorf("%s: unexpected id %q; wanted %v", label, id, want)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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
