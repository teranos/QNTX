//go:build cgo && rustpostgres

package postgrescgo

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// open is a store in a namespace no other test touches, on the Postgres
// QNTX_POSTGRES_URL names. Unset is a failure: a test that passes without a
// database says nothing.
func open(t *testing.T) *PostgresStore {
	t.Helper()
	url := os.Getenv("QNTX_POSTGRES_URL")
	if url == "" {
		t.Fatal("QNTX_POSTGRES_URL names no Postgres to test against")
	}
	ns := fmt.Sprintf("go%d_%d", os.Getpid(), time.Now().UnixNano())
	store, err := NewPostgresStore(url, "", ns)
	if err != nil {
		t.Fatalf("NewPostgresStore in %s: %v", ns, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return store
}

func attestation(id, subject string, at time.Time) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{"knows"},
		Contexts:   []string{"work"},
		Actors:     []string{"human:bob"},
		Timestamp:  at,
		Source:     "test",
		Attributes: map[string]any{"k": "v"},
		CreatedAt:  at,
	}
}

func TestAnAttestationRoundTrips(t *testing.T) {
	store := open(t)
	at := time.UnixMilli(1_700_000_000_000)
	if err := store.CreateAttestation(attestation("AS-1", "ALICE", at)); err != nil {
		t.Fatalf("CreateAttestation: %v", err)
	}
	got, err := store.GetAttestation("AS-1")
	if err != nil {
		t.Fatalf("GetAttestation: %v", err)
	}
	if got.Subjects[0] != "ALICE" || !got.Timestamp.Equal(at) || got.Attributes["k"] != "v" {
		t.Errorf("got %+v", got)
	}
	if !store.AttestationExists("AS-1") || store.AttestationExists("AS-2") {
		t.Error("AttestationExists answered wrong")
	}
	if _, err := store.GetAttestation("AS-2"); err == nil {
		t.Error("GetAttestation of an id not held answered no error")
	}
}

// TestTheRecordTakesWhatALandingFileSends: storage.SendOut and storage.TakeIn
// reach the record through WriteFile and GetAttestations.
func TestTheRecordTakesWhatALandingFileSends(t *testing.T) {
	store := open(t)
	early, late := time.UnixMilli(1000), time.UnixMilli(2000)
	batch := []*types.As{attestation("AS-1", "ALICE", early), attestation("AS-2", "BOB", late)}
	for range 2 {
		wrote, err := store.WriteFile(batch)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if wrote != len(batch) {
			t.Errorf("WriteFile = %d, want %d", wrote, len(batch))
		}
	}
	if n, err := store.CountAttestations(); err != nil || n != 2 {
		t.Errorf("CountAttestations = %d, %v; want 2", n, err)
	}
	since, err := store.GetAttestations(ats.AttestationFilter{TimeStart: &late})
	if err != nil {
		t.Fatalf("GetAttestations: %v", err)
	}
	if len(since) != 1 || since[0].ID != "AS-2" {
		t.Errorf("GetAttestations since %v = %v", late, since)
	}
}

func TestSchema(t *testing.T) {
	url := os.Getenv("QNTX_POSTGRES_URL")
	if url == "" {
		t.Fatal("QNTX_POSTGRES_URL names no Postgres to test against")
	}
	tables, version, err := Schema(url, "", fmt.Sprintf("schema%d", os.Getpid()))
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if len(tables) != 2 || tables[0] != "attestations" || tables[1] != "schema_migrations" {
		t.Errorf("tables = %v", tables)
	}
	if version == "" {
		t.Error("Schema named no server version")
	}
}
