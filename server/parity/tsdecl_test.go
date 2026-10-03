package parity

import (
	"slices"
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
