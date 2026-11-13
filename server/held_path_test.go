package server

import (
	"path/filepath"
	"testing"
)

// 「[Opus 5.5] A subsystem's DuckDB is kept beside the node's database, and its name is one」
// 「[Opus 5.5] segment of the path.」
func TestASubsystemsDuckDBIsKeptBesideTheNodesDatabase(t *testing.T) {
	got, err := heldPath("/var/lib/qntx/qntx.db", "quotes")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/var/lib/qntx", "subsystems", "quotes.duckdb"); got != want {
		t.Errorf("held at %s, want %s", got, want)
	}
	for _, name := range []string{"", ".", "..", "../system", `a\b`, "it's"} {
		if _, err := heldPath("/var/lib/qntx/qntx.db", name); err == nil {
			t.Errorf("%q was taken as a name to keep a DuckDB under", name)
		}
	}
}
