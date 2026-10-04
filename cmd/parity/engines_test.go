package main

import (
	"os"
	"strings"
	"testing"

	"github.com/teranos/QNTX/server/parity"
)

// TestDuckDBIsHeldToItsPin holds EXPECTED_DUCKDB_VERSION to
// server/parity/duckdb_<version>_<rev>. ats-duckdb refuses to open against any
// libduckdb but that one, so the constant is the version the node links.
func TestDuckDBIsHeldToItsPin(t *testing.T) {
	pinned, err := parity.Pinned("../../server/parity", "duckdb")
	if err != nil {
		t.Fatal(err)
	}

	const path = "../../crates/ats-duckdb/src/lib.rs"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	const opener = `const EXPECTED_DUCKDB_VERSION: &str = "`
	_, rest, ok := strings.Cut(string(body), opener)
	if !ok {
		t.Fatalf("%s declares no EXPECTED_DUCKDB_VERSION", path)
	}
	expected, _, ok := strings.Cut(rest, `"`)
	if !ok {
		t.Fatalf("EXPECTED_DUCKDB_VERSION in %s does not close", path)
	}
	if strings.TrimPrefix(expected, "v") != pinned {
		t.Errorf("ats-duckdb expects DuckDB %s, the pin is %s", expected, pinned)
	}
}
