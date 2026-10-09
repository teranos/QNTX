package parity

import (
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A JSON Schema's shape: a property of each form the pinned MCP schema writes.
const jsonShapes = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$defs": {
    "Meta": {"type": "object", "additionalProperties": {}},
    "Kind": {"type": "string", "enum": ["a", "b"]},
    "Either": {"anyOf": [{"$ref": "#/$defs/Meta"}, {"type": "string"}]},
    "Seen": {
      "type": "object",
      "required": ["visit", "views"],
      "properties": {
        "visit": {"type": "string"},
        "views": {"type": "integer", "minimum": 0},
        "tags": {"type": "array", "items": {"type": "string"}},
        "_meta": {"$ref": "#/$defs/Meta"},
        "kind": {"$ref": "#/$defs/Kind"},
        "either": {"$ref": "#/$defs/Either"},
        "both": {"allOf": [{"$ref": "#/$defs/Meta"}, {}]},
        "anything": {},
        "shape": {"type": "object", "properties": {"type": {"const": "object", "type": "string"}}}
      }
    }
  }
}`

func readJSON(t *testing.T, body string) Schema {
	t.Helper()
	schema, err := ParseJSONSchema("schema.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// A definition with properties is a model, its properties in the order the
// schema writes them, each under the schema's own word for it.
func TestParseJSONSchema_Columns(t *testing.T) {
	schema := readJSON(t, jsonShapes)
	if len(schema.Models) != 1 || schema.Models[0].Name != "Seen" {
		t.Fatalf("models are %+v", schema.Models)
	}
	var got []string
	var required []string
	for _, c := range schema.Models[0].Columns {
		word := c.Name + ":" + c.Type
		if c.List {
			word += "[]"
		}
		got = append(got, word)
		if c.Required {
			required = append(required, c.Name)
		}
	}
	want := []string{"visit:string", "views:integer", "tags:string[]", "_meta:Meta", "kind:Kind",
		"either:Either", "both:Meta & any", "anything:any", "shape:object"}
	if !slices.Equal(got, want) {
		t.Errorf("Seen is %v", got)
	}
	if !slices.Equal(required, []string{"visit", "views"}) {
		t.Errorf("Seen requires %v", required)
	}
}

// A field fits a column by the types the column resolves to, every
// definition it refers to read.
func TestHold_JSONSchemaKinds(t *testing.T) {
	signum := visitFollows(
		"protocol.Visit.visit", "Seen.visit",
		"protocol.Visit.views", "Seen.views",
		"protocol.Visit.entry_path", "Seen.tags",
		"protocol.Visit.started", "Seen.kind",
		"protocol.Visit.market", "Seen.either",
		"protocol.Visit.slug", "Seen.anything",
		"protocol.Visit.exit_path", "Seen._meta",
	)
	p, refused := Hold(signum, "", "ref", readJSON(t, jsonShapes))
	if refused != nil {
		t.Fatal(refused)
	}
	for _, column := range []string{"visit", "views", "kind", "either", "anything"} {
		if i := itemOf(t, p, "Seen", column); !i.Conforms() {
			t.Errorf("%s departs: %v", column, i.Departs)
		}
	}
	if i := itemOf(t, p, "Seen", "tags"); i.Conforms() || !strings.Contains(strings.Join(i.Departs, ";"), "a list in the schema") {
		t.Errorf("one value in a list departs by %v", i.Departs)
	}
	if i := itemOf(t, p, "Seen", "_meta"); i.Conforms() || !strings.Contains(strings.Join(i.Departs, ";"), "Meta in the schema") {
		t.Errorf("a string in an object departs by %v", i.Departs)
	}
	if !slices.Equal(p.Required, []string{}) {
		t.Errorf("required and unfollowed is %v", p.Required)
	}
}

// What a schema says that cannot be read is the schema's, and named.
func TestParseJSONSchema_Refuses(t *testing.T) {
	for name, body := range map[string]string{
		"no $defs":         `{"type": "object"}`,
		"no properties":    `{"$defs": {"Kind": {"type": "string"}}}`,
		"a dangling $ref":  `{"$defs": {"Seen": {"properties": {"a": {"$ref": "#/$defs/Nosuch"}}}}}`,
		"a $ref elsewhere": `{"$defs": {"Seen": {"properties": {"a": {"$ref": "other.json#/Seen"}}}}}`,
		"a $ref to itself": `{"$defs": {"Loop": {"$ref": "#/$defs/Loop"}, "Seen": {"properties": {"a": {"$ref": "#/$defs/Loop"}}}}}`,
	} {
		if _, err := ParseJSONSchema("schema.json", []byte(body)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}

// mcp is read from its schema.json: Tool as the spec has it, with what the
// spec requires.
func TestReference_MCP(t *testing.T) {
	schema, refused := Reference("mcp")
	if refused != nil {
		t.Fatal(refused)
	}
	var tool *Model
	for i, m := range schema.Models {
		if m.Name == "Tool" {
			tool = &schema.Models[i]
		}
	}
	if tool == nil {
		t.Fatal("mcp has no Tool")
	}
	var names, required []string
	for _, c := range tool.Columns {
		names = append(names, c.Name)
		if c.Required {
			required = append(required, c.Name)
		}
	}
	if !slices.Equal(names, []string{"_meta", "annotations", "description", "icons", "inputSchema", "name", "outputSchema", "title"}) {
		t.Errorf("Tool is %v", names)
	}
	if !slices.Equal(required, []string{"inputSchema", "name"}) {
		t.Errorf("Tool requires %v", required)
	}
	if icons := tool.Columns[3]; !icons.List || icons.Type != "Icon" {
		t.Errorf("icons is %+v", icons)
	}
}

// A repeated field keyed by a field of its element is one object, a property
// per element: it is followed into an object without departing, and what it is
// keyed by is held to the element.
func TestHold_KeyedBy(t *testing.T) {
	signum := &protocol.Signum{Name: "s", Follows: []*protocol.Follows{{Reference: "ref", Columns: []*protocol.Corresponds{
		{Field: "protocol.Sigil.takes", Column: "Seen.shape", KeyedBy: "name"},
		{Field: "protocol.Sigil.gives", Column: "Seen._meta", KeyedBy: "nope"},
		{Field: "protocol.Sigil.name", Column: "Seen.anything", KeyedBy: "name"},
		{Field: "protocol.Sigil.takes", Column: "Seen.tags", KeyedBy: "name"},
		{Field: "protocol.Sigil.does", Column: "Seen.visit"},
	}}}}
	p, refused := Hold(signum, "", "ref", readJSON(t, jsonShapes))
	if refused != nil {
		t.Fatal(refused)
	}
	if i := itemOf(t, p, "Seen", "shape"); !i.Conforms() {
		t.Errorf("a list keyed by name in an object departs: %v", i.Departs)
	}
	for column, want := range map[string]string{
		"_meta":    "keyed by nope, and protocol.Field has no text field nope",
		"anything": "keyed by name, and protocol.Sigil.name is not a repeated message",
		"tags":     "a list in the schema, and protocol.Sigil.takes keyed by name is one object",
	} {
		if i := itemOf(t, p, "Seen", column); i.Conforms() || !strings.Contains(strings.Join(i.Departs, ";"), want) {
			t.Errorf("%s departs by %v, not %q", column, i.Departs, want)
		}
	}
}
