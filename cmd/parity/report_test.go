//go:build rustduckdb

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teranos/QNTX/server/parity"
)

// TestDuckDBSchema_Attestations: the crate builds the attestations prefix from
// a constant, which the prefix scan cannot see; DuckDB holds attestations
// because its migration creates the table, and that is where this answer
// comes from.
func TestDuckDBSchema_Attestations(t *testing.T) {
	tables, err := DuckDBSchema()
	if err != nil {
		t.Fatalf("DuckDBSchema: %v", err)
	}
	if !tables["attestations"] {
		t.Errorf("attestations missing from what ats-duckdb's migrations leave: %v", tables)
	}
	if tables["schema_migrations"] {
		t.Error("schema_migrations present: it is the runner's own bookkeeping")
	}
}

// The node embeds what make parity wrote, so the committed file is either what
// the source says now or it is a lie about what QNTX persists. A migration or
// a statement that changes the picture and no `make parity` fails here.
func TestTheWrittenStorageIsWhatTheSourceSays(t *testing.T) {
	root := filepath.Join("..", "..")
	things, err := Report(root, "crates/ats-duckdb/src")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Written(root, things)
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(filepath.Join(root, parity.StorageFile))
	if err != nil {
		t.Fatalf("%s has never been written; run make parity: %v", parity.StorageFile, err)
	}
	if string(fresh) != string(written) {
		t.Errorf("%s is not what the source says; run make parity", parity.StorageFile)
	}
}
