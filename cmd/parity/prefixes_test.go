package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestPrefixesNamed_JoinForm is how tokens.rs names its prefix. Missing it
// means a finished backend still prints NO.
func TestPrefixesNamed_JoinForm(t *testing.T) {
	names := PrefixesNamed(`PathBuf::from(base).join("access_tokens")`)
	if !slices.Contains(names, "access_tokens") {
		t.Errorf("got %v, want access_tokens", names)
	}
}

// TestPrefixesNamed_FormatForm is how lib.rs names the attestations prefix.
func TestPrefixesNamed_FormatForm(t *testing.T) {
	names := PrefixesNamed(`format!("{}/attestations/{}-{}.parquet", base, ms, id)`)
	if !slices.Contains(names, "attestations") {
		t.Errorf("got %v, want attestations", names)
	}
}

// TestPrefixesNamed_RejectsNonSegments: a prefix names one directory. Anything
// else read as one puts a line on the picture for a thing QNTX does not store,
// and every invented line costs someone a search for code that isn't there.
func TestPrefixesNamed_RejectsNonSegments(t *testing.T) {
	for _, source := range []string{
		`format!("{}/{}", base, name)`,
		`.join("Cargo.toml")`,
		`.join("a/b")`,
		`.join("")`,
	} {
		if names := PrefixesNamed(source); len(names) != 0 {
			t.Errorf("PrefixesNamed(%q) = %v, want none", source, names)
		}
	}
}

// TestObjectPrefixes_ReadsTheCrate walks the crate the way the tool does.
func TestObjectPrefixes_ReadsTheCrate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "tokens.rs", `fn prefix(&self) -> PathBuf {
    PathBuf::from(base).join("access_tokens")
}`)
	write(t, dir, "lib.rs", `let glob = format!("{}/attestations/*.parquet", base);`)
	write(t, dir, "notes.md", `.join("not_rust")`)

	prefixes, err := ObjectPrefixes(dir)
	if err != nil {
		t.Fatalf("ObjectPrefixes: %v", err)
	}
	if !prefixes["access_tokens"] || !prefixes["attestations"] {
		t.Errorf("got %v, want access_tokens and attestations", prefixes)
	}
	if prefixes["not_rust"] {
		t.Error("read a prefix out of a non-Rust file")
	}
}

// TestCovered_ContractFoldsIntoStorage: TokenStore yields "tokens" while
// storage calls the same thing "access_tokens". Carrying both would print one
// line NO/NO forever next to the line that says the work is done.
func TestCovered_ContractFoldsIntoStorage(t *testing.T) {
	present := map[string]*Thing{"access_tokens": {Name: "access_tokens"}}

	if !covered("tokens", present) {
		t.Error("tokens not folded into access_tokens")
	}
	if covered("watchers", present) {
		t.Error("watchers folded into access_tokens, which does not contain it")
	}
}

// TestCovered_DoesNotSwallowItself guards the case where a contract and a
// storage name are identical — that is one thing, already on the picture, and
// must not be dropped as if something else covered it.
func TestCovered_DoesNotSwallowItself(t *testing.T) {
	present := map[string]*Thing{"watchers": {Name: "watchers"}}
	if covered("watchers", present) {
		t.Error("watchers reported as covered by itself")
	}
}

// TestObjectPrefixes_MissingDirFails: pointed at nothing, the tool must say so
// rather than report a parquet column of all NO, which reads as real work.
func TestObjectPrefixes_MissingDirFails(t *testing.T) {
	if _, err := ObjectPrefixes(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected an error for a missing crate directory, got none")
	}
}

// TestObjectPrefixes_ReadsTheRealCrate is the one that catches a rename. If
// the crate stops joining "access_tokens", or the scan stops seeing it, the
// parquet column silently reverts to NO and nobody learns why.
func TestObjectPrefixes_ReadsTheRealCrate(t *testing.T) {
	if _, err := os.Stat("../../crates/qntx-duckdb/src"); err != nil {
		t.Skipf("crate not present: %v", err)
	}
	prefixes, err := ObjectPrefixes("../../crates/qntx-duckdb/src")
	if err != nil {
		t.Fatalf("ObjectPrefixes: %v", err)
	}
	for _, want := range []string{"access_tokens", "attestations"} {
		if !prefixes[want] {
			t.Errorf("%s missing from the real crate scan: %v", want, prefixes)
		}
	}
}
