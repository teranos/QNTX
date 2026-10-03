package parity

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"testing"
)

// protocol.says.json is what the protocol's .proto files say, as they say it
// now: a comment changed and not written fails here by name.
func TestProtocolSaysIsWhatTheSourceSays(t *testing.T) {
	read, err := ProtocolSays(os.DirFS("../../plugin/grpc/protocol"))
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]string
	if err := json.Unmarshal(protocolSaysJSON, &written); err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Sorted(maps.Keys(read)) {
		if written[name] != read[name] {
			t.Errorf("%s says %q in its .proto, and protocol.says.json has %q: run make says", name, read[name], written[name])
		}
	}
	for name := range written {
		if _, ok := read[name]; !ok {
			t.Errorf("protocol.says.json has %s, which says nothing in its .proto: run make says", name)
		}
	}
}

// A comment is prose: its lines joined, a blank line a paragraph, and what AIP
// marks between (-- and --) left out.
func TestWords(t *testing.T) {
	for comment, want := range map[string]string{
		" A human-readable name\n for the skill.\n":                "A human-readable name for the skill.",
		" One.\n\n Two.\n":                                         "One.\n\nTwo.",
		" (-- api-linter: off\n     not for readers --)\n Kept.\n": "Kept.",
		"": "",
	} {
		if got := words(comment); got != want {
			t.Errorf("words(%q) is %q, want %q", comment, got, want)
		}
	}
}

// What a2a's .proto says of AgentSkill and its fields is read with them, and
// what it marks as not for readers is not.
func TestReference_A2ASays(t *testing.T) {
	schema, refused := Reference("a2a")
	if refused != nil {
		t.Fatal(refused)
	}
	skill := modelNamed(t, schema, "AgentSkill")
	if skill.Says != "Represents a distinct capability or function that an agent can perform." {
		t.Errorf("AgentSkill says %q", skill.Says)
	}
	if skill.Columns[0].Says != "A unique identifier for the agent's skill." {
		t.Errorf("AgentSkill.id says %q", skill.Columns[0].Says)
	}
	signature := modelNamed(t, schema, "AgentCardSignature")
	if got := signature.Columns[0].Says; got != "Required. The protected JWS header for the signature. This is always a base64url-encoded JSON object." {
		t.Errorf("AgentCardSignature.protected says %q", got)
	}
}

// What MCP's schema.json says of Tool and its properties is their description.
func TestReference_MCPSays(t *testing.T) {
	schema, refused := Reference("mcp")
	if refused != nil {
		t.Fatal(refused)
	}
	tool := modelNamed(t, schema, "Tool")
	if tool.Says != "Definition for a tool the client can call." {
		t.Errorf("Tool says %q", tool.Says)
	}
	for _, c := range tool.Columns {
		if c.Name == "name" && c.Says != "Intended for programmatic or logical use, but used as a display name in past specs or fallback (if title isn't present)." {
			t.Errorf("Tool.name says %q", c.Says)
		}
		if c.Name == "_meta" && c.Says != "" {
			t.Errorf("Tool._meta, which the schema says nothing of, says %q", c.Says)
		}
	}
}

// A /// line is what a schema.prisma says of the model or field below it; a //
// line is not.
func TestParsePrisma_Says(t *testing.T) {
	models := writeSchema(t, `
/// One page view.
model Seen {
  /// Who saw it.
  id   String
  // not documentation
  path String
}
`)
	if models[0].Says != "One page view." {
		t.Errorf("Seen says %q", models[0].Says)
	}
	if models[0].Columns[0].Says != "Who saw it." || models[0].Columns[1].Says != "" {
		t.Errorf("Seen's columns say %+v", models[0].Columns)
	}
}

func modelNamed(t *testing.T, schema Schema, name string) Model {
	t.Helper()
	for _, m := range schema.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no %s", name)
	return Model{}
}

// Umami's schema says nothing; its API document speaks for the models words
// links it to, a column taking the words of the property of its name, and
// saying where they were read.
func TestReference_UmamiSaysFromItsAPI(t *testing.T) {
	schema, refused := Reference("umami")
	if refused != nil {
		t.Fatal(refused)
	}
	session := modelNamed(t, schema, "Session")
	for _, c := range session.Columns {
		if c.Name == "screen" && (c.Says != "Screen resolution of the visitor device." || c.SaysFrom != "openapi.json · WebsiteSession.screen") {
			t.Errorf("Session.screen says %q, from %q", c.Says, c.SaysFrom)
		}
	}
	event := modelNamed(t, schema, "WebsiteEvent")
	for _, c := range event.Columns {
		if c.Name == "gclid" && (c.Says != "" || c.SaysFrom != "") {
			t.Errorf("WebsiteEvent.gclid, which the API document says nothing of, says %q from %q", c.Says, c.SaysFrom)
		}
	}
	if modelNamed(t, schema, "EventData").Columns[0].Says != "" {
		t.Error("EventData, which words links to no schema, has words")
	}
}

// A words line that names what is not there is the pin's to fix, by line.
func TestApplyAPIWords_Refuses(t *testing.T) {
	doc := []byte(`{"components":{"schemas":{"WebsiteSession":{"properties":{"id":{"description":"Unique."}}}}}}`)
	for name, links := range map[string]string{
		"no such model":  "Nosuch WebsiteSession\n",
		"no such schema": "Session Nosuch\n",
		"not a pair":     "Session\n",
	} {
		models := []Model{{Name: "Session", Columns: []Column{{Name: "id"}}}}
		if err := applyAPIWords("umami", []byte(links), doc, models); err == nil {
			t.Errorf("%s: applied without error", name)
		}
	}
	models := []Model{{Name: "Session", Columns: []Column{{Name: "id"}, {Name: "os"}}}}
	if err := applyAPIWords("umami", []byte("# a comment\nSession WebsiteSession\n"), doc, models); err != nil {
		t.Fatal(err)
	}
	if models[0].Columns[0].Says != "Unique." || models[0].Columns[1].Says != "" {
		t.Errorf("Session's columns say %+v", models[0].Columns)
	}
}
