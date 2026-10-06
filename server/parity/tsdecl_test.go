package parity

import (
	"slices"
	"strings"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// An exported object type is a model, each field of it a column, required
// unless it is marked optional, saying what its JSDoc says. A signature is no
// field, and a type with no field is no model.
func TestParseTypeScript(t *testing.T) {
	models, err := ParseTypeScript("index.d.ts", []byte(`/** What is sent. */
export type Sent = {
  /**
   * Page url
   *
   * @description normalized from the location
   * @example '/home'
   * @deprecated not this
   */
  url?: string;
  website: string;
  tags?: string[];
  track: {
    (): Promise<void>;
  };
};
export interface Bag {
  [key: string]: string;
}
export type Alias = Other;
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Name != "Sent" || models[0].Says != "What is sent." {
		t.Fatalf("models are %+v", models)
	}
	var got []string
	for _, c := range models[0].Columns {
		word := c.Name + ":" + c.Type
		if c.List {
			word += "[]"
		}
		if c.Required {
			word += "!"
		}
		got = append(got, word)
	}
	if !slices.Equal(got, []string{"url:string", "website:string!", "tags:string[]", "track:object!"}) {
		t.Errorf("Sent is %v", got)
	}
	if says := models[0].Columns[0].Says; says != "Page url\n\nnormalized from the location\n\ne.g. '/home'" {
		t.Errorf("url says %q", says)
	}
}

// Umami is read from its record and its tracker, as one reference: the
// tracker's columns held to TypeScript's types, saying where they were read.
func TestReference_UmamiReadsItsTracker(t *testing.T) {
	schema, refused := Reference("umami")
	if refused != nil {
		t.Fatal(refused)
	}
	modelNamed(t, schema, "WebsiteEvent")
	tracked := modelNamed(t, schema, "TrackedProperties")
	for _, c := range tracked.Columns {
		if c.Name == "screen" && c.SaysFrom != "index.d.ts · TrackedProperties.screen" {
			t.Errorf("screen says it was read from %q", c.SaysFrom)
		}
	}
	signum := &protocol.Signum{
		Name:    "s",
		Follows: []*protocol.Follows{{Reference: "umami", Columns: []*protocol.Corresponds{{Field: "protocol.Visit.views", Column: "TrackedProperties.url"}}}},
		Sigils:  []*protocol.Sigil{{Name: "visits", Gives: []*protocol.Field{{Name: "visits", Message: "protocol.Visit"}}}},
	}
	p, refused := Hold(signum, "", "umami", schema)
	if refused != nil {
		t.Fatal(refused)
	}
	if i := itemOf(t, p, "TrackedProperties", "url"); i.Conforms() {
		t.Errorf("a uint32 in a string conforms: %+v", i)
	}
}

// A file written with `export declare`, as Claude Code's SDK is: a type that
// is a base and what it adds carries the base's columns first, and a column it
// writes again is its own.
func TestParseTypeScript_DeclaredAndIntersected(t *testing.T) {
	models, err := ParseTypeScript("sdk.d.ts", []byte(`export declare type Base = {
    session_id: string;
    agent_type?: string;
};
export declare type Asked = Base & {
    hook_event_name: 'Asked';
    prompt: string;
    agent_type: string;
};
export declare type Switched = (Base & {
    hook_event_name: 'Switched';
}) & {
    to_model: string;
};
export declare interface Held {
    readonly id: string;
}
export declare type Either = Asked | Switched;
`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	var names []string
	for _, m := range models {
		names = append(names, m.Name)
		for _, c := range m.Columns {
			word := c.Name + ":" + c.Type
			if c.Required {
				word += "!"
			}
			got[m.Name] = append(got[m.Name], word)
		}
	}
	if !slices.Equal(names, []string{"Base", "Asked", "Switched", "Held"}) {
		t.Fatalf("models are %v", names)
	}
	want := map[string][]string{
		"Base":     {"session_id:string!", "agent_type:string"},
		"Asked":    {"session_id:string!", "hook_event_name:'Asked'!", "prompt:string!", "agent_type:string!"},
		"Switched": {"session_id:string!", "agent_type:string", "hook_event_name:'Switched'!", "to_model:string!"},
		"Held":     {"id:string!"},
	}
	for name, columns := range want {
		if !slices.Equal(got[name], columns) {
			t.Errorf("%s is %v, want %v", name, got[name], columns)
		}
	}
}

// The shape of a declaration file is its models and their columns written back
// as declarations with none of its words: read again, it is the same models.
func TestShapeReadsBackAsTheSameModels(t *testing.T) {
	models, err := ParseTypeScript("sdk.d.ts", []byte(`/** What is asked. */
export declare type Base = {
    /** Which session. */
    session_id: string;
    effort?: {
        level: string;
    };
};
export declare type Asked = Base & {
    prompt: string;
    tags?: string[];
};
`))
	if err != nil {
		t.Fatal(err)
	}
	shape := Shape(models)
	if strings.Contains(string(shape), "What is asked") || strings.Contains(string(shape), "Which session") {
		t.Errorf("the shape carries the declarations' words:\n%s", shape)
	}
	again, err := ParseTypeScript("sdk.shape.d.ts", shape)
	if err != nil {
		t.Fatalf("the shape did not read: %v\n%s", err, shape)
	}
	if len(again) != len(models) {
		t.Fatalf("the shape has %d models, and the declarations %d", len(again), len(models))
	}
	for i, m := range models {
		if again[i].Name != m.Name || len(again[i].Columns) != len(m.Columns) {
			t.Fatalf("%s read back as %+v", m.Name, again[i])
		}
		for j, c := range m.Columns {
			back := again[i].Columns[j]
			if back.Name != c.Name || back.Type != c.Type || back.List != c.List || back.Required != c.Required {
				t.Errorf("%s.%s read back as %+v, want %+v", m.Name, c.Name, back, c)
			}
		}
	}
}
