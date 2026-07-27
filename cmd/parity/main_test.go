package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReplaySchema_FinalStateNotStatements is the bug the previous version
// shipped: it scraped CREATE TABLE out of the migration files, so it saw every
// statement in history and a rebuild's scratch table read as a thing QNTX
// persists. Replaying to final state cannot make that mistake.
func TestReplaySchema_FinalStateNotStatements(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001_create_aliases.sql", `CREATE TABLE aliases (alias TEXT, target TEXT);`)
	write(t, dir, "002_rebuild_aliases.sql", `
		CREATE TABLE aliases_new (alias TEXT COLLATE NOCASE, target TEXT);
		INSERT INTO aliases_new SELECT alias, target FROM aliases;
		DROP TABLE aliases;
		ALTER TABLE aliases_new RENAME TO aliases;`)

	tables := replay(t, dir)
	if !tables["aliases"] {
		t.Error("aliases missing: it survives the rebuild and is a thing QNTX persists")
	}
	if tables["aliases_new"] {
		t.Error("aliases_new present: it does not exist once the migrations finish")
	}
}

// TestReplaySchema_SkipsRunnerBookkeeping keeps schema_migrations off the
// picture. Every backend has it by construction, so it distinguishes nothing.
func TestReplaySchema_SkipsRunnerBookkeeping(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "000_schema_migrations.sql", `CREATE TABLE schema_migrations (version TEXT PRIMARY KEY);`)
	write(t, dir, "001_watchers.sql", `CREATE TABLE watchers (id TEXT);`)

	tables := replay(t, dir)
	if tables["schema_migrations"] {
		t.Error("schema_migrations present: it is the runner's own bookkeeping")
	}
	if !tables["watchers"] {
		t.Error("watchers missing")
	}
}

// TestReplaySchema_ShadowTablesAreNotThings: a virtual table materialises
// several tables of its own internals. The developer created one thing and the
// picture must show one line, not five.
func TestReplaySchema_ShadowTablesAreNotThings(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001_vectors.sql", `CREATE VIRTUAL TABLE vec_embeddings USING vec0(embedding float[4]);`)

	tables := replay(t, dir)
	if !tables["vec_embeddings"] {
		t.Fatalf("vec_embeddings missing from %v", tables)
	}
	for name := range tables {
		if name != "vec_embeddings" && strings.HasPrefix(name, "vec_embeddings_") {
			t.Errorf("%s present: it is vec_embeddings' internal storage, not a thing QNTX persists", name)
		}
	}
}

// TestReplaySchema_AcceptsDuckDBTypes guards the assumption the parquet column
// rests on: DuckDB migrations replay under SQLite. VARCHAR[] breaks first if
// that stops holding.
func TestReplaySchema_AcceptsDuckDBTypes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001_attestations.sql", `
		CREATE TABLE IF NOT EXISTS attestations (
			id VARCHAR PRIMARY KEY,
			subjects VARCHAR[] NOT NULL,
			timestamp BIGINT NOT NULL,
			signature BLOB
		);`)

	if !replay(t, dir)["attestations"] {
		t.Error("attestations missing from replayed DuckDB schema")
	}
}

// TestReplaySchema_BadMigrationFails: a migration that will not run is a
// broken tool, not a backend holding nothing. Swallowing it prints NO for
// every thing, and that reads exactly like real work to do.
func TestReplaySchema_BadMigrationFails(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001_broken.sql", `CREATE TABLE (((;`)

	if _, err := ReplaySchema(dir); err == nil {
		t.Fatal("expected an error from an unrunnable migration, got none")
	}
}

// TestRender_FourStates: all four combinations read off the picture, and NO/NO
// draws a line instead of vanishing.
func TestRender_FourStates(t *testing.T) {
	out := Render([]Thing{
		{Name: "access_tokens", SQLite: false, Parquet: false},
		{Name: "embeddings", SQLite: true, Parquet: false},
		{Name: "attestations", SQLite: true, Parquet: true},
		{Name: "future_thing", SQLite: false, Parquet: true},
	})

	for _, want := range []string{
		"access_tokens  NO       NO",
		"embeddings     YES      NO",
		"attestations   YES      YES",
		"future_thing   NO       YES",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "SQLITE | PARQUET") {
		t.Errorf("missing backend header in:\n%s", out)
	}
}

// TestRender_NoRankingNoScore: the picture states presence and nothing else.
// A count of what is left makes one backend the baseline the other is measured
// against, which is wrong in both directions — parquet is the reference
// implementation for some things and SQLite for others.
func TestRender_NoRankingNoScore(t *testing.T) {
	out := strings.ToLower(Render([]Thing{
		{Name: "attestations", SQLite: true, Parquet: true},
		{Name: "access_tokens"},
	}))
	for _, banned := range []string{" of ", "missing", "gap", "parity", "%"} {
		if strings.Contains(out, banned) {
			t.Errorf("output ranks or scores (%q):\n%s", banned, out)
		}
	}
}

func replay(t *testing.T, dir string) map[string]bool {
	t.Helper()
	tables, err := ReplaySchema(dir)
	if err != nil {
		t.Fatalf("ReplaySchema: %v", err)
	}
	return tables
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
