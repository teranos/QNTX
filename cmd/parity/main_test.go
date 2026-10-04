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

// TestRender_FourStates: all four combinations read off the picture, and NO/NO
// draws a line instead of vanishing.
func TestRender_FourStates(t *testing.T) {
	out := Render([]Thing{
		{Name: "access_tokens", Node: false, Record: false},
		{Name: "embeddings", Node: true, Record: false},
		{Name: "attestations", Node: true, Record: true},
		{Name: "future_thing", Node: false, Record: true},
	})

	for _, want := range []string{
		"access_tokens  NO            NO",
		"embeddings     YES           NO",
		"attestations   YES           YES",
		"future_thing   NO            YES",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "ON THE NODE | IN THE RECORD") {
		t.Errorf("missing header in:\n%s", out)
	}
}

// TestRender_RebuiltIsNotNo: a table the take-in rebuilds from attestations is
// not lost with the host, and NO in the record column would say it is.
func TestRender_RebuiltIsNotNo(t *testing.T) {
	out := Render([]Thing{{Name: "attestation_subjects", Node: true, Rebuilt: true}})
	if !strings.Contains(out, "attestation_subjects  YES           rebuilt from attestations") {
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
// A count of what is left makes one backend the baseline the other is measured
// against, which is wrong in both directions — parquet is the reference
// implementation for some things and SQLite for others.
func TestRender_NoRankingNoScore(t *testing.T) {
	out := strings.ToLower(Render([]Thing{
		{Name: "attestations", Node: true, Record: true},
		{Name: "attestation_subjects", Node: true, Rebuilt: true},
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
