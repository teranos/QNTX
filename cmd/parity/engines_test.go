package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/parity"
)

// TestTheDuckDBCrateIsHeldToItsPin: cargo cannot read the pin, so the duckdb
// crate's version is the one place the DuckDB version is written again.
// ats-duckdb's build.rs gives the rest of the crate the pin itself.
func TestTheDuckDBCrateIsHeldToItsPin(t *testing.T) {
	root := filepath.Join("..", "..")
	pinned, err := parity.Pinned(filepath.Join(root, filepath.Dir(parity.StorageFile)), "duckdb")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "crates", "ats-duckdb", "Cargo.toml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	const opener = `duckdb = { version = "=`
	_, rest, ok := strings.Cut(string(body), opener)
	if !ok {
		t.Fatalf("%s names no duckdb crate version", path)
	}
	crate, _, ok := strings.Cut(rest, `"`)
	if !ok {
		t.Fatalf("the duckdb crate version in %s does not close", path)
	}
	if crate != pinned {
		t.Errorf("%s asks for the duckdb crate at %s, the pin is %s", path, crate, pinned)
	}
}
