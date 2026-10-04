package main

import (
	"encoding/json"
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

// TestTheSupabasePostgresInputIsHeldToItsPin: a flake input cannot read the
// pin, so flake.nix names the commit again and flake.lock locks it. The lock
// is what Nix builds, so the lock is held to the pin.
func TestTheSupabasePostgresInputIsHeldToItsPin(t *testing.T) {
	root := filepath.Join("..", "..")
	pinned, err := parity.PinnedAt(filepath.Join(root, filepath.Dir(parity.StorageFile)), "postgres")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "flake.lock")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var lock struct {
		Nodes map[string]struct {
			Locked struct {
				Owner string `json:"owner"`
				Repo  string `json:"repo"`
				Rev   string `json:"rev"`
			} `json:"locked"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &lock); err != nil {
		t.Fatalf("%s did not parse: %v", path, err)
	}
	node, ok := lock.Nodes["supabase-postgres"]
	if !ok {
		t.Fatalf("%s locks no supabase-postgres", path)
	}
	if node.Locked.Owner != "supabase" || node.Locked.Repo != "postgres" {
		t.Errorf("%s locks supabase-postgres to %s/%s", path, node.Locked.Owner, node.Locked.Repo)
	}
	if !strings.HasPrefix(node.Locked.Rev, pinned) {
		t.Errorf("%s locks supabase/postgres at %s, the pin is %s", path, node.Locked.Rev, pinned)
	}
}
