package main

import (
	"strings"
	"testing"
)

// TestTablesNamed_EveryClauseThatReachesAThing: a statement welds Go to SQLite
// whether it reads, writes or joins. Missing a clause hides a callsite that
// has to move before the backend column can change.
func TestTablesNamed_EveryClauseThatReachesAThing(t *testing.T) {
	things := map[string]bool{"attestations": true, "watchers": true}

	cases := map[string]string{
		"SELECT id FROM attestations":            "attestations",
		"INSERT INTO watchers (id) VALUES (?)":   "watchers",
		"UPDATE watchers SET name = ?":           "watchers",
		"SELECT * FROM x JOIN attestations ON 1": "attestations",
		"delete from watchers where id = ?":      "watchers",
	}
	for sql, want := range cases {
		got := TablesNamed(sql, things)
		if len(got) != 1 || got[0] != want {
			t.Errorf("TablesNamed(%q) = %v, want [%s]", sql, got, want)
		}
	}
}

// TestTablesNamed_UnknownNamesAreNotThings: a CTE, alias or subquery name is
// not a thing QNTX persists. Listing one puts a file under a line it does not
// touch, and the whole value of the sites is that you can open them.
func TestTablesNamed_UnknownNamesAreNotThings(t *testing.T) {
	things := map[string]bool{"attestations": true}

	for _, sql := range []string{
		"WITH recent AS (SELECT 1) SELECT * FROM recent",
		"SELECT * FROM sqlite_master",
		"SELECT value FROM json_each(a.subjects)",
	} {
		if got := TablesNamed(sql, things); len(got) != 0 {
			t.Errorf("TablesNamed(%q) = %v, want none", sql, got)
		}
	}
}

// TestTablesNamed_QuotedAndParenthesised: SQL quoting and a parenthesis run up
// against the name constantly. Losing the name loses the site.
func TestTablesNamed_QuotedAndParenthesised(t *testing.T) {
	things := map[string]bool{"attestations": true}

	for _, sql := range []string{
		`SELECT id FROM "attestations" WHERE id = ?`,
		"INSERT INTO attestations(id) VALUES (?)",
		"SELECT id FROM attestations;",
	} {
		got := TablesNamed(sql, things)
		if len(got) != 1 || got[0] != "attestations" {
			t.Errorf("TablesNamed(%q) = %v, want [attestations]", sql, got)
		}
	}
}

// TestStatementSites_ReportsFileAndLine: the point of a site is that you can
// open it. A name without a line is not actionable.
func TestStatementSites_ReportsFileAndLine(t *testing.T) {
	root := fixture(t, map[string]string{
		"server/handlers.go": `package server

func load(db any) {
	query(db, "SELECT id FROM watchers")
}`,
	})

	sites, err := StatementSites(root, map[string]bool{"watchers": true})
	if err != nil {
		t.Fatalf("StatementSites: %v", err)
	}
	found := sites["watchers"]
	if len(found) != 1 {
		t.Fatalf("got %v, want one site", found)
	}
	if !strings.HasSuffix(found[0].File, "server/handlers.go") || found[0].Line != 4 {
		t.Errorf("got %s:%d, want server/handlers.go:4", found[0].File, found[0].Line)
	}
}

// TestStatementSites_SkipsTests: a test writing SQL is not production code
// welded to SQLite, and burying the real callsites under fixtures makes the
// list unreadable.
func TestStatementSites_SkipsTests(t *testing.T) {
	root := fixture(t, map[string]string{
		"server/handlers_test.go": `package server

func seed(db any) {
	query(db, "INSERT INTO watchers (id) VALUES (?)")
}`,
	})

	sites, err := StatementSites(root, map[string]bool{"watchers": true})
	if err != nil {
		t.Fatalf("StatementSites: %v", err)
	}
	if len(sites["watchers"]) != 0 {
		t.Errorf("got %v from a _test.go file, want none", sites["watchers"])
	}
}

// TestRender_SitesNestUnderTheirThing: the sites belong to the line above
// them. Flattened, they are 210 paths with nothing saying what they are for.
func TestRender_SitesNestUnderTheirThing(t *testing.T) {
	out := Render([]Thing{
		{Name: "watchers", SQLite: true, Sites: []Site{
			{File: "ats/storage/watcher_store.go", Line: 155},
		}},
		{Name: "tokens"},
	})

	watchers := strings.Index(out, "watchers")
	site := strings.Index(out, "ats/storage/watcher_store.go:155")
	tokens := strings.Index(out, "tokens")
	if site < watchers || site > tokens {
		t.Errorf("site is not between its thing and the next:\n%s", out)
	}
	if !strings.Contains(out, "      ats/storage/watcher_store.go:155") {
		t.Errorf("site is not indented under its thing:\n%s", out)
	}
}
