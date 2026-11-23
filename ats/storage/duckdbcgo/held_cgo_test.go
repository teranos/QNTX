//go:build cgo && rustduckdb

package duckdbcgo

import (
	"path/filepath"
	"testing"
)

// 「[Opus 5.5] A subsystem's own table, made by its migration, holds what was written」
// 「[Opus 5.5] across a close and an open.」
func TestAHeldDuckDBKeepsItsRowsAcrossOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subsystems", "quotes.duckdb")
	migrations := []HeldMigration{{Version: "quotes-001", SQL: "CREATE TABLE reads (id VARCHAR PRIMARY KEY, doc VARCHAR, read_at BIGINT);"}}

	held, err := OpenHeld(path, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := held.Exec("INSERT INTO reads VALUES (?, ?, ?)", "AS-1", "teranos/QNTX/AGENTS.md", int64(1_700_000_000_000)); err != nil || n != 1 {
		t.Fatalf("insert changed %d rows: %v", n, err)
	}
	held.Close()

	held, err = OpenHeld(path, migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	var rows []struct {
		ID     string `json:"id"`
		ReadAt int64  `json:"read_at"`
	}
	if err := held.Query(&rows, "SELECT id, read_at FROM reads WHERE doc = ?", "teranos/QNTX/AGENTS.md"); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "AS-1" || rows[0].ReadAt != 1_700_000_000_000 {
		t.Errorf("rows: %+v, want AS-1 read at 1700000000000", rows)
	}
}

// 「[Opus 5.5] What DuckDB refused rides back as the error.」
func TestAHeldDuckDBSaysWhatItRefused(t *testing.T) {
	held, err := OpenHeld(filepath.Join(t.TempDir(), "q.duckdb"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	var rows []map[string]any
	if err := held.Query(&rows, "SELECT * FROM no_such_table"); err == nil {
		t.Error("a query of a table that is not there answered no error")
	}
}
