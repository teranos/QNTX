package parity

import (
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

func writeSchema(t *testing.T, body string) []Model {
	t.Helper()
	models, err := ParsePrisma("schema.prisma", []byte(body))
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
	p, err := Hold(signum, "", "ref", Prisma(writeSchema(t, shapes)))
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
	p, err := Hold(visitFollows("protocol.Visit.visit", "Seen.id", "protocol.Visit.visitor", "Whole.v"), "", "ref", Prisma(models))
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
	p, err := Hold(signum, "visits", "ref", Prisma(models))
	if err != nil {
		t.Fatal(err)
	}
	if len(itemOf(t, p, "Untouched", "a").Followed) != 0 {
		t.Errorf("visits was held by an Arrival field")
	}
	if _, ok := p.Unfollowed["protocol.Arrival"]; ok {
		t.Errorf("visits carries no Arrival, and Arrival is out of its spec")
	}
	if _, err := Hold(signum, "list", "ref", Prisma(models)); err == nil {
		t.Errorf("list carries no message and was held")
	}
	if _, err := Hold(signum, "nosuch", "ref", Prisma(models)); err == nil {
		t.Errorf("a sigil the signum lacks was held")
	}
}

// What follows no column, and a column the schema lacks, are out of spec.
func TestHold_OutOfSpec(t *testing.T) {
	p, err := Hold(visitFollows("protocol.Visit.visit", "Seen.id", "protocol.Visit.visitor", "Seen.gone"), "", "ref", Prisma(writeSchema(t, shapes)))
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
	if _, err := Hold(visitFollows("protocol.Visit.visit", "Else.id"), "", "ref", Prisma(writeSchema(t, shapes))); err == nil {
		t.Errorf("a schema with none of the columns was held")
	}
}

// A reference is named, and one the signum does not follow is refused as the
// caller's to fix; so is a signum that follows nothing.
func TestHold_TheReferenceNamed(t *testing.T) {
	models := writeSchema(t, shapes)
	_, refused := Hold(visitFollows("protocol.Visit.visit", "Seen.id"), "", "other", Prisma(models))
	if refused == nil || refused.GetParam() != "reference" || !strings.Contains(refused.GetSays(), "it follows ref") {
		t.Errorf("a reference not followed was not refused by name: %v", refused)
	}
	_, refused = Hold(&protocol.Signum{Name: "bare"}, "", "ref", Prisma(models))
	if refused == nil || refused.GetSays() != "bare follows nothing" {
		t.Errorf("a signum that follows nothing was held: %v", refused)
	}
}

// The node holds umami as pinned, and refuses a reference it holds nothing of.
func TestReference_Pinned(t *testing.T) {
	schema, refused := Reference("umami")
	if refused != nil {
		t.Fatal(refused)
	}
	if !slices.ContainsFunc(schema.Models, func(m Model) bool { return m.Name == "WebsiteEvent" }) {
		t.Errorf("umami has no WebsiteEvent")
	}
	if _, refused := Reference("nosuch"); refused == nil || refused.GetParam() != "reference" {
		t.Errorf("a reference the node does not hold was not refused: %v", refused)
	}
}

// a2a is read from its .proto: AgentSkill as the spec has it, with what the
// spec marks REQUIRED.
func TestReference_A2A(t *testing.T) {
	schema, refused := Reference("a2a")
	if refused != nil {
		t.Fatal(refused)
	}
	var skill *Model
	for i, m := range schema.Models {
		if m.Name == "AgentSkill" {
			skill = &schema.Models[i]
		}
	}
	if skill == nil {
		t.Fatal("a2a has no AgentSkill")
	}
	var names, required []string
	for _, c := range skill.Columns {
		names = append(names, c.Name)
		if c.Required {
			required = append(required, c.Name)
		}
	}
	if !slices.Equal(names, []string{"id", "name", "description", "tags", "examples", "input_modes", "output_modes", "security_requirements"}) {
		t.Errorf("AgentSkill is %v", names)
	}
	if !slices.Equal(required, []string{"id", "name", "description", "tags"}) {
		t.Errorf("AgentSkill requires %v", required)
	}
	if !skill.Columns[3].List || skill.Columns[3].Type != "string" {
		t.Errorf("tags is %+v", skill.Columns[3])
	}
}
