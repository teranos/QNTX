package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShadowOf: a virtual table materialises several tables of its own
// internals. The developer created one thing and the picture must show one
// line, not five.
func TestShadowOf(t *testing.T) {
	virtual := []string{"vec_embeddings"}
	if !shadowOf("vec_embeddings_chunks", virtual) {
		t.Error("vec_embeddings_chunks read as a thing: it is vec_embeddings' internal storage")
	}
	if shadowOf("vec_embeddings", virtual) {
		t.Error("vec_embeddings read as its own shadow")
	}
	if shadowOf("embeddings", virtual) {
		t.Error("embeddings read as a shadow of vec_embeddings")
	}
}

// TestRender_EachEngine: each engine's column reads off the picture, and a
// line of NO is drawn instead of vanishing.
func TestRender_EachEngine(t *testing.T) {
	out := Render([]Thing{
		{Name: "access_tokens"},
		{Name: "embeddings", SQLite: true},
		{Name: "attestations", SQLite: true, DuckDB: true},
		{Name: "future_thing", DuckDB: true},
	})

	for _, want := range []string{
		"access_tokens  NO      NO\n",
		"embeddings     YES     NO\n",
		"attestations   YES     YES\n",
		"future_thing   NO      YES\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "SQLITE  DUCKDB") {
		t.Errorf("missing header in:\n%s", out)
	}
}

// TestRender_Rebuilt: a table whose rows cascade from attestations says so.
func TestRender_Rebuilt(t *testing.T) {
	out := Render([]Thing{{Name: "attestation_subjects", SQLite: true, Rebuilt: true}})
	if !strings.Contains(out, "attestation_subjects  YES     NO      rebuilt from attestations") {
		t.Errorf("rebuilt table not said so in:\n%s", out)
	}
}

// TestSQLiteSchema_RebuiltIsReadFromTheForeignKeys: the four junction tables
// cascade from attestations in the real schema, and nothing else does.
func TestSQLiteSchema_RebuiltIsReadFromTheForeignKeys(t *testing.T) {
	tables, rebuilt, _, err := SQLiteSchema()
	if err != nil {
		t.Fatalf("SQLiteSchema: %v", err)
	}
	want := []string{"attestation_actors", "attestation_contexts", "attestation_predicates", "attestation_subjects"}
	for _, name := range want {
		if !tables[name] || !rebuilt[name] {
			t.Errorf("%s: table %v, rebuilt %v, want both", name, tables[name], rebuilt[name])
		}
	}
	if len(rebuilt) != len(want) {
		t.Errorf("rebuilt = %v, want exactly %v", rebuilt, want)
	}
	if rebuilt["attestations"] {
		t.Error("attestations read as rebuilt from itself")
	}
}

// TestRender_NoRankingNoScore: the picture states presence and nothing else.
// A count of what is left makes one backend the baseline the others are
// measured against, which is wrong in every direction.
func TestRender_NoRankingNoScore(t *testing.T) {
	out := strings.ToLower(Render([]Thing{
		{Name: "attestations", SQLite: true, DuckDB: true},
		{Name: "attestation_subjects", SQLite: true, Rebuilt: true},
		{Name: "access_tokens"},
	}))
	for _, banned := range []string{" of ", "missing", "gap", "parity", "%"} {
		if strings.Contains(out, banned) {
			t.Errorf("output ranks or scores (%q):\n%s", banned, out)
		}
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
