package parity

import (
	"slices"
	"testing"
)

// A skill is named by its path in the taxonomy, and its id is derived from
// that path as OASF derives it.
func TestOASFSkills(t *testing.T) {
	skills, refused := OASFSkills()
	if refused != nil {
		t.Fatal(refused)
	}
	if len(skills) != 513 {
		t.Errorf("the pinned taxonomy holds %d skills", len(skills))
	}
	for name, id := range map[string]uint32{
		"software_engineering": 6,
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
