package storage

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/db"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/errors"
)

func quickDevAttestation(id string) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{"PLUGIN"},
		Predicates: []string{"github"},
		Contexts:   []string{"https://github.com/teranos/qntx-github"},
		Actors:     []string{"qntx"},
		Timestamp:  time.UnixMilli(1791046018311),
		Source:     "qntx",
		Attributes: map[string]any{"enabled": true, "note": "<a & b>"},
		CreatedAt:  time.UnixMilli(1791046018000),
	}
}

// What QuickDev keeps, it reads back, by id and by filter; an id kept twice is
// refused.
func TestSQLRawStoreKeepsAndReadsBack(t *testing.T) {
	store := NewSQLRawStore(qntxtest.CreateTestDB(t))
	as := quickDevAttestation("AS-quickdev-000000000001")
	if err := store.CreateAttestation(as); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAttestation(as); err == nil {
		t.Error("the same id was kept twice")
	}
	if !store.AttestationExists(as.ID) || store.AttestationExists("AS-quickdev-ffffffffffff") {
		t.Error("AttestationExists does not tell kept from not")
	}
	if count, err := store.CountAttestations(); err != nil || count != 1 {
		t.Errorf("counted %d, %v", count, err)
	}
	got, err := store.GetAttestation(as.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Timestamp.Equal(as.Timestamp) || got.Predicates[0] != "github" || got.Attributes["note"] != "<a & b>" {
		t.Errorf("read back %+v", got)
	}
	if _, err := store.GetAttestation("AS-quickdev-ffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a miss answered %v", err)
	}
	found, err := store.GetAttestations(ats.AttestationFilter{Limit: ats.EveryRow, Subjects: []string{"PLUGIN"}, Predicates: []string{"github"}})
	if err != nil || len(found) != 1 {
		t.Errorf("the filter found %d, %v", len(found), err)
	}
	byIDs, err := store.GetAttestationsByIDs([]string{as.ID})
	if err != nil || len(byIDs) != 1 {
		t.Errorf("by id found %d, %v", len(byIDs), err)
	}
}

// The row QuickDev writes is the row ats-sqlite writes, column for column, and
// so are its junction rows: Go's reads were written against Rust's rows.
func TestSQLRawStoreWritesTheRowAtsSqliteWrites(t *testing.T) {
	as := quickDevAttestation("AS-quickdev-000000000002")

	rustPath := filepath.Join(t.TempDir(), "rust.db")
	rust, err := sqlitecgo.NewFileStore(rustPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := rust.CreateAttestation(as); err != nil {
		t.Fatal(err)
	}
	if err := rust.Close(); err != nil {
		t.Fatal(err)
	}
	rustDB, err := db.OpenReadOnly(rustPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rustDB.Close() })

	goDB := qntxtest.CreateTestDB(t)
	if err := NewSQLRawStore(goDB).CreateAttestation(as); err != nil {
		t.Fatal(err)
	}

	row := func(d *sql.DB) []any {
		t.Helper()
		var id, subjects, predicates, contexts, actors, timestamp, source, createdAt string
		var attributes, signerDID sql.NullString
		var signature []byte
		if err := d.QueryRow("SELECT id, subjects, predicates, contexts, actors, CAST(timestamp AS TEXT), source, attributes, CAST(created_at AS TEXT), signature, signer_did FROM attestations WHERE id = ?", as.ID).
			Scan(&id, &subjects, &predicates, &contexts, &actors, &timestamp, &source, &attributes, &createdAt, &signature, &signerDID); err != nil {
			t.Fatal(err)
		}
		var junction int
		for _, j := range junctions {
			var n int
			if err := d.QueryRow("SELECT COUNT(*) FROM "+j.table+" WHERE attestation_id = ?", as.ID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			junction += n
		}
		// Rust keeps attributes in a HashMap, so their key order is serde's
		// and not fixed: they are the same when they decode the same.
		var decoded map[string]any
		if err := json.Unmarshal([]byte(attributes.String), &decoded); err != nil {
			t.Fatal(err)
		}
		attributesRead, err := json.Marshal(decoded)
		if err != nil {
			t.Fatal(err)
		}
		return []any{id, subjects, predicates, contexts, actors, timestamp, source, string(attributesRead), createdAt, string(signature), signerDID, junction}
	}
	rustRow, goRow := row(rustDB), row(goDB)
	names := []string{"id", "subjects", "predicates", "contexts", "actors", "timestamp", "source", "attributes", "created_at", "signature", "signer_did", "junction rows"}
	for i, name := range names {
		if rustRow[i] != goRow[i] {
			t.Errorf("%s: ats-sqlite wrote %v, QuickDev wrote %v", name, rustRow[i], goRow[i])
		}
	}
}
