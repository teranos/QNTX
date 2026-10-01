package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server"
)

func writeSchema(t *testing.T, body string) []Model {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.prisma")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	models, err := ParsePrisma(path)
	if err != nil {
		t.Fatal(err)
	}
	return models
}

// A schema's shape: Visit's fields held against a column of each kind a field
// can meet.
const shapes = `
model Seen {
  id       String   @id @db.Uuid
  started  DateTime
  views    Int
  tags     String[]
  owner    Other    @relation(fields: [id], references: [id])
}

model Untouched {
  a String
  b String
}
`

func visitFollows(pairs ...string) *protocol.Signum {
	f := &protocol.Follows{Reference: "ref"}
	for i := 0; i < len(pairs); i += 2 {
		f.Columns = append(f.Columns, &protocol.Corresponds{Field: pairs[i], Column: pairs[i+1]})
	}
	return &protocol.Signum{
		Name:    "s",
		Follows: []*protocol.Follows{f},
		Sigils: []*protocol.Sigil{
			{Name: "visits", Gives: []*protocol.Field{{Name: "visits", Message: "protocol.Visit"}}},
			{Name: "list", Gives: []*protocol.Field{{Name: "rows"}}},
		},
	}
}

func itemOf(t *testing.T, p Parity, model, column string) Item {
	t.Helper()
	for _, c := range p.Clades {
		if c.Model != model {
			continue
		}
		for _, i := range c.Items {
			if i.Column == column {
				return i
			}
		}
	}
	t.Fatalf("no %s.%s", model, column)
	return Item{}
}

// A relation is not a column, and a list is one.
func TestParsePrisma_ColumnsAreScalars(t *testing.T) {
	models := writeSchema(t, shapes)
	var names []string
	for _, c := range models[0].Columns {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"id", "started", "views", "tags"}) {
		t.Errorf("columns are %v", names)
	}
	if !models[0].Columns[3].List || models[0].Columns[0].List {
		t.Errorf("tags is a list and id is not: %+v", models[0].Columns)
	}
}

// Each kind and cardinality meets a column it fits and one it does not.
func TestHold_KindAndCardinality(t *testing.T) {
	signum := visitFollows(
		"protocol.Visit.visit", "Seen.id",
		"protocol.Visit.started", "Seen.started",
		"protocol.Visit.views", "Seen.views",
		"protocol.Visit.entry_path", "Seen.tags",
	)
	p, err := Hold(signum, "", writeSchema(t, shapes))
	if err != nil {
		t.Fatal(err)
	}
	if i := itemOf(t, p, "Seen", "id"); !i.Conforms() {
		t.Errorf("a string in a String departs: %v", i.Departs)
	}
	if i := itemOf(t, p, "Seen", "views"); !i.Conforms() {
		t.Errorf("a uint32 in an Int departs: %v", i.Departs)
	}
	if i := itemOf(t, p, "Seen", "started"); i.Conforms() || !strings.Contains(strings.Join(i.Departs, ";"), "DateTime in the schema") {
		t.Errorf("a string in a DateTime departs by %v", i.Departs)
	}
	if i := itemOf(t, p, "Seen", "tags"); i.Conforms() || !strings.Contains(strings.Join(i.Departs, ";"), "a list in the schema") {
		t.Errorf("one value in a list departs by %v", i.Departs)
	}
}

// A clade nothing follows is one line at 0; a clade anything follows opens and
// lists every column, the zeros too; a clade at 100 is not shown until -all.
func TestRender_Clades(t *testing.T) {
	models := writeSchema(t, shapes+"\nmodel Whole {\n  v String\n}\n")
	p, err := Hold(visitFollows("protocol.Visit.visit", "Seen.id", "protocol.Visit.visitor", "Whole.v"), "", models)
	if err != nil {
		t.Fatal(err)
	}
	out := p.Render(false)
	if !strings.Contains(out, "  Untouched") || strings.Contains(out, "    a ") {
		t.Errorf("a clade nothing follows is not one line:\n%s", out)
	}
	for _, column := range []string{"    id ", "    started ", "    views ", "    tags "} {
		if !strings.Contains(out, column) {
			t.Errorf("%q of an open clade is not listed:\n%s", column, out)
		}
	}
	if strings.Contains(out, "  Whole") || !strings.Contains(out, "1 at 100 not shown") {
		t.Errorf("a clade at 100 is shown:\n%s", out)
	}
	if !strings.Contains(p.Render(true), "  Whole") {
		t.Errorf("-all does not show the clade at 100")
	}
}

// A sigil is held by the message it carries alone, and one that carries none
// cannot be held.
func TestHold_OneSigil(t *testing.T) {
	models := writeSchema(t, shapes)
	signum := visitFollows("protocol.Visit.visit", "Seen.id", "protocol.Arrival.path", "Untouched.a")
	p, err := Hold(signum, "visits", models)
	if err != nil {
		t.Fatal(err)
	}
	if len(itemOf(t, p, "Untouched", "a").Followed) != 0 {
		t.Errorf("visits was held by an Arrival field")
	}
	if _, ok := p.Unfollowed["protocol.Arrival"]; ok {
		t.Errorf("visits carries no Arrival, and Arrival is out of its spec")
	}
	if _, err := Hold(signum, "list", models); err == nil {
		t.Errorf("list carries no message and was held")
	}
	if _, err := Hold(signum, "nosuch", models); err == nil {
		t.Errorf("a sigil the signum lacks was held")
	}
}

// What follows no column, and a column the schema lacks, are out of spec.
func TestHold_OutOfSpec(t *testing.T) {
	p, err := Hold(visitFollows("protocol.Visit.visit", "Seen.id", "protocol.Visit.visitor", "Seen.gone"), "", writeSchema(t, shapes))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(p.Unfollowed["protocol.Visit"], "visit") || !slices.Contains(p.Unfollowed["protocol.Visit"], "bounce") {
		t.Errorf("unfollowed is %v", p.Unfollowed["protocol.Visit"])
	}
	if !slices.Equal(p.Missing, []string{"protocol.Visit.visitor → Seen.gone"}) {
		t.Errorf("missing is %v", p.Missing)
	}
}

// A schema that has none of a signum's columns is not one it follows.
func TestHold_ASchemaItDoesNotFollow(t *testing.T) {
	if _, err := Hold(visitFollows("protocol.Visit.visit", "Else.id"), "", writeSchema(t, shapes)); err == nil {
		t.Errorf("a schema with none of the columns was held")
	}
}

// Every signum that follows a reference can be held to it: here, staands and
// the pinned Umami schema, where every column staands follows exists.
func TestStaandsHoldsToThePinnedUmami(t *testing.T) {
	models, err := ParsePrisma(filepath.Join("umami_v3.3.1_ca661c7", "schema.prisma"))
	if err != nil {
		t.Fatal(err)
	}
	signum, err := findSignum(server.DeclaredSigna(), "staands")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Hold(signum, "", models)
	if err != nil {
		t.Fatal(err)
	}
	if p.Reference != "umami" {
		t.Errorf("staands was held to %s", p.Reference)
	}
	if len(p.Missing) != 0 {
		t.Errorf("staands follows columns the pinned schema lacks: %v", p.Missing)
	}
	// What datapunt has to give before this command is let go of.
	want, err := os.ReadFile(filepath.Join("umami_v3.3.1_ca661c7", "staands"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Render(true); got != string(want) {
		t.Errorf("staands no longer reads as umami_v3.3.1_ca661c7/staands records; got\n%s", got)
	}
}
