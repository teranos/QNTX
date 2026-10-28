package parity

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A skill is named by its path in the taxonomy, and its id is derived from
// that path as OASF derives it.
func TestOASFSkills(t *testing.T) {
	skills, refused := OASFSkills()
	if refused != nil {
		t.Fatal(refused)
	}
	if len(skills) != 495 {
		t.Errorf("the pinned taxonomy holds %d skills a Record can name", len(skills))
	}
	if _, held := skills["software_engineering"]; held {
		t.Error("software_engineering is a category, and a Record names none")
	}
	for name, id := range map[string]uint32{
		"software_engineering/version_control":                                  610,
		"software_engineering/version_control/pull_request_management":          61003,
		"cybersecurity/identity_access_management/authorization_access_control": 100502,
		"tool_use_automation/workflow_automation":                               1704,
	} {
		if got, found := skills[name]; !found || got != id {
			t.Errorf("%s is %d (held: %v), and OASF derives %d", name, got, found, id)
		}
	}
	ids := map[uint32]string{}
	for name, id := range skills {
		if other, twice := ids[id]; twice {
			t.Errorf("%s and %s are both %d", name, other, id)
		}
		ids[id] = name
	}
}

// A module is named and numbered as a skill is: integration/a2a is 203 and
// core/observability 101, as OASF's own server answers them.
func TestOASFModules(t *testing.T) {
	modules, refused := OASFModules()
	if refused != nil {
		t.Fatal(refused)
	}
	for name, id := range map[string]uint32{"integration/a2a": 203, "integration/mcp": 202, "core/observability": 101} {
		if got, found := modules[name]; !found || got != id {
			t.Errorf("%s is %d (held: %v), and OASF derives %d", name, got, found, id)
		}
	}
}

// What OASF requires of a Record, and the schema version it names, are read
// from the pin.
func TestOASFRecordRequirements(t *testing.T) {
	required, refused := OASFRequired("record")
	if refused != nil {
		t.Fatal(refused)
	}
	if !slices.Equal(required, []string{"name", "version", "schema_version", "description", "authors", "created_at", "skills"}) {
		t.Errorf("a Record requires %v", required)
	}
	version, refused := OASFSchemaVersion()
	if refused != nil || version != "1.1.0" {
		t.Errorf("the schema version is %q: %v", version, refused)
	}
}

// The skills and modules a Record can name, with their ids, are exactly the
// ones the Record schema OASF's server gives holds, each a name and an id it
// fixes.
func TestOASFClassesAreTheRecordSchemas(t *testing.T) {
	raw, refused := OASFRecordSchema()
	if refused != nil {
		t.Fatal(refused)
	}
	var schema struct {
		Defs map[string]map[string]struct {
			Properties struct {
				Name struct{ Const string } `json:"name"`
				ID   struct{ Const uint32 } `json:"id"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	for family, derive := range map[string]func() (map[string]uint32, *protocol.Refusal){"skills": OASFSkills, "modules": OASFModules} {
		derived, refused := derive()
		if refused != nil {
			t.Fatal(refused)
		}
		fixed := map[string]uint32{}
		for _, def := range schema.Defs[family] {
			fixed[def.Properties.Name.Const] = def.Properties.ID.Const
		}
		if len(fixed) == 0 || !maps.Equal(fixed, derived) {
			t.Errorf("%s: the schema fixes %d, and %d are derived from the taxonomy", family, len(fixed), len(derived))
			for name, id := range fixed {
				if derived[name] != id {
					t.Errorf("%s: %s is %d in the schema and %d derived", family, name, id, derived[name])
				}
			}
		}
	}
}
