package types

import (
	"encoding/json"
	"testing"

	"github.com/teranos/QNTX/ats/attrs"
)

// MockAttestationStore for testing
type MockAttestationStore struct {
	attestations []*As
}

func (m *MockAttestationStore) CreateAttestation(as *As) error {
	m.attestations = append(m.attestations, as)
	return nil
}

func TestAttestType_Basic(t *testing.T) {
	store := &MockAttestationStore{}

	err := AttestType(store, "document", "test-source", map[string]interface{}{
		"display_color": "#3498db",
		"display_label": "Document",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.attestations) != 1 {
		t.Fatalf("expected 1 attestation, got %d", len(store.attestations))
	}

	as := store.attestations[0]

	// Check basic structure
	if as.Subjects[0] != "document" {
		t.Errorf("expected subject 'document', got %v", as.Subjects[0])
	}

	if as.Predicates[0] != "type" {
		t.Errorf("expected predicate 'type', got %v", as.Predicates[0])
	}

	// Type attestations have no context
	if len(as.Contexts) != 0 {
		t.Errorf("expected no contexts, got %v", as.Contexts)
	}
}

func TestAttestType_SelfCertifyingActor(t *testing.T) {
	store := &MockAttestationStore{}

	err := AttestType(store, "artifact", "test-source", map[string]interface{}{
		"display_color": "#9b59b6",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	as := store.attestations[0]

	// Type should be its own actor (self-certifying in typespace)
	if len(as.Actors) != 1 {
		t.Errorf("expected 1 actor, got %d", len(as.Actors))
	}

	if as.Actors[0] != "artifact" {
		t.Errorf("expected self-certifying actor 'artifact', got %v", as.Actors[0])
	}

	// Actor should match the subject (type name)
	if as.Actors[0] != as.Subjects[0] {
		t.Errorf("actor should match subject (self-certifying), got actor=%v subject=%v",
			as.Actors[0], as.Subjects[0])
	}
}

func TestAttestType_EmptyTypeName(t *testing.T) {
	store := &MockAttestationStore{}

	err := AttestType(store, "", "test-source", map[string]interface{}{
		"display_color": "#000000",
	})

	if err == nil {
		t.Error("expected error for empty type name, got nil")
	}

	if err.Error() != "typeName cannot be empty" {
		t.Errorf("unexpected error message: %v", err)
	}

	// Should not have created any attestations
	if len(store.attestations) != 0 {
		t.Errorf("expected 0 attestations, got %d", len(store.attestations))
	}
}

func TestAttestType_EmptySource(t *testing.T) {
	store := &MockAttestationStore{}

	err := AttestType(store, "document", "", map[string]interface{}{
		"display_color": "#000000",
	})

	if err == nil {
		t.Error("expected error for empty source, got nil")
	}

	if err.Error() != "source cannot be empty" {
		t.Errorf("unexpected error message: %v", err)
	}

	// Should not have created any attestations
	if len(store.attestations) != 0 {
		t.Errorf("expected 0 attestations, got %d", len(store.attestations))
	}
}

func TestEnsureTypes_OpacityHandling(t *testing.T) {
	store := &MockAttestationStore{}

	// Helper to create float64 pointer
	float64Ptr := func(v float64) *float64 { return &v }

	typeDefs := []TypeDef{
		{
			Name:    "opaque",
			Label:   "Opaque Type",
			Color:   "#ff0000",
			Opacity: nil, // Should default to 1.0
		},
		{
			Name:    "transparent",
			Label:   "Transparent Type",
			Color:   "#00ff00",
			Opacity: float64Ptr(0.0), // Explicitly transparent
		},
		{
			Name:    "semitransparent",
			Label:   "Semi-transparent Type",
			Color:   "#0000ff",
			Opacity: float64Ptr(0.5), // Explicitly semi-transparent
		},
	}

	err := EnsureTypes(store, SaysNothing, "test-source", typeDefs...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.attestations) != 3 {
		t.Fatalf("expected 3 attestations, got %d", len(store.attestations))
	}

	// Test opaque type (nil opacity → default 1.0)
	opaqueAttestation := store.attestations[0]
	if opacity, ok := opaqueAttestation.Attributes["opacity"].(float64); !ok || opacity != 1.0 {
		t.Errorf("opaque type: expected opacity 1.0, got %v", opaqueAttestation.Attributes["opacity"])
	}

	// Test transparent type (explicit 0.0)
	transparentAttestation := store.attestations[1]
	if opacity, ok := transparentAttestation.Attributes["opacity"].(float64); !ok || opacity != 0.0 {
		t.Errorf("transparent type: expected opacity 0.0, got %v", transparentAttestation.Attributes["opacity"])
	}

	// Test semi-transparent type (explicit 0.5)
	semiAttestation := store.attestations[2]
	if opacity, ok := semiAttestation.Attributes["opacity"].(float64); !ok || opacity != 0.5 {
		t.Errorf("semi-transparent type: expected opacity 0.5, got %v", semiAttestation.Attributes["opacity"])
	}
}

func TestEnsureTypes_RichStringFields(t *testing.T) {
	store := &MockAttestationStore{}

	typeDefs := []TypeDef{
		{
			Name:             "candidate",
			Label:            "Candidate",
			Color:            "#e74c3c",
			RichStringFields: []string{"notes", "description"},
		},
		{
			Name:             "jd",
			Label:            "Job Description",
			Color:            "#27ae60",
			RichStringFields: []string{"description", "requirements"},
		},
		{
			Name:  "simple",
			Label: "Simple Type",
			Color: "#3498db",
			// No RichStringFields
		},
	}

	err := EnsureTypes(store, SaysNothing, "test-source", typeDefs...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.attestations) != 3 {
		t.Fatalf("expected 3 attestations, got %d", len(store.attestations))
	}

	// Test candidate type with rich_string_fields
	candidateAttestation := store.attestations[0]
	richFields, ok := candidateAttestation.Attributes["rich_string_fields"].([]string)
	if !ok {
		t.Errorf("candidate: expected rich_string_fields to be []string, got %T", candidateAttestation.Attributes["rich_string_fields"])
	}
	if len(richFields) != 2 {
		t.Errorf("candidate: expected 2 rich_string_fields, got %d", len(richFields))
	}
	if richFields[0] != "notes" || richFields[1] != "description" {
		t.Errorf("candidate: expected [notes, description], got %v", richFields)
	}

	// Test jd type with different rich_string_fields
	jdAttestation := store.attestations[1]
	jdRichFields, ok := jdAttestation.Attributes["rich_string_fields"].([]string)
	if !ok {
		t.Errorf("jd: expected rich_string_fields to be []string, got %T", jdAttestation.Attributes["rich_string_fields"])
	}
	if len(jdRichFields) != 2 {
		t.Errorf("jd: expected 2 rich_string_fields, got %d", len(jdRichFields))
	}
	if jdRichFields[0] != "description" || jdRichFields[1] != "requirements" {
		t.Errorf("jd: expected [description, requirements], got %v", jdRichFields)
	}

	// Test simple type with NO rich_string_fields
	simpleAttestation := store.attestations[2]
	if _, exists := simpleAttestation.Attributes["rich_string_fields"]; exists {
		t.Errorf("simple: expected rich_string_fields to be absent, but found: %v", simpleAttestation.Attributes["rich_string_fields"])
	}
}

// TestIsExistenceAttestation validates detection of pure existence claims vs relationships
// Critical for graph building: existence attestations don't create links, relationships do
func TestIsExistenceAttestation(t *testing.T) {
	// Pure existence: "as alice" becomes "as alice _ _"
	existence := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"_"},
		Contexts:   []string{"_"},
	}
	if !existence.IsExistenceAttestation() {
		t.Error("Expected existence attestation for 'as alice _ _'")
	}

	// Relationship: "as alice works_at acme"
	relationship := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"works_at"},
		Contexts:   []string{"acme"},
	}
	if relationship.IsExistenceAttestation() {
		t.Error("Expected NOT existence attestation for relationship")
	}

	// Has predicate but default context: "as alice is _"
	predicateOnly := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"is"},
		Contexts:   []string{"_"},
	}
	if predicateOnly.IsExistenceAttestation() {
		t.Error("Expected NOT existence attestation when predicate is not '_'")
	}

	// Has context but default predicate: "as alice _ engineer"
	contextOnly := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"_"},
		Contexts:   []string{"engineer"},
	}
	if contextOnly.IsExistenceAttestation() {
		t.Error("Expected NOT existence attestation when context is not '_'")
	}

	// Multiple predicates (even if one is _)
	multiPredicate := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"_", "is"},
		Contexts:   []string{"_"},
	}
	if multiPredicate.IsExistenceAttestation() {
		t.Error("Expected NOT existence attestation with multiple predicates")
	}

	// Multiple contexts (even if one is _)
	multiContext := &As{
		Subjects:   []string{"alice"},
		Predicates: []string{"_"},
		Contexts:   []string{"_", "engineer"},
	}
	if multiContext.IsExistenceAttestation() {
		t.Error("Expected NOT existence attestation with multiple contexts")
	}
}

func TestEnsureTypes_ArrayFields(t *testing.T) {
	store := &MockAttestationStore{}

	typeDefs := []TypeDef{
		{
			Name:        "repository",
			Label:       "Code Repository",
			Color:       "#6e5494",
			ArrayFields: []string{"languages", "topics", "contributors"},
		},
		{
			Name:  "commit",
			Label: "Commit",
			Color: "#34495e",
			// No ArrayFields
		},
	}

	err := EnsureTypes(store, SaysNothing, "test-source", typeDefs...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.attestations) != 2 {
		t.Fatalf("expected 2 attestations, got %d", len(store.attestations))
	}

	// Test repository type with array_fields
	repoAttestation := store.attestations[0]
	arrayFields, ok := repoAttestation.Attributes["array_fields"].([]string)
	if !ok {
		t.Errorf("repository: expected array_fields to be []string, got %T", repoAttestation.Attributes["array_fields"])
	}
	if len(arrayFields) != 3 {
		t.Errorf("repository: expected 3 array_fields, got %d", len(arrayFields))
	}
	expected := []string{"languages", "topics", "contributors"}
	for i, field := range expected {
		if arrayFields[i] != field {
			t.Errorf("repository: expected array_fields[%d] = %s, got %s", i, field, arrayFields[i])
		}
	}

	// Test commit type with NO array_fields
	commitAttestation := store.attestations[1]
	if _, exists := commitAttestation.Attributes["array_fields"]; exists {
		t.Errorf("commit: expected array_fields to be absent, but found: %v", commitAttestation.Attributes["array_fields"])
	}
}

// saying is what a store already said, in the form it comes back in: through
// JSON, where a []string is a []any and 1.0 is 1.
func saying(t *testing.T, def TypeDef) Says {
	t.Helper()
	if def.Opacity == nil {
		one := 1.0
		def.Opacity = &one
	}
	wire, err := json.Marshal(attrs.From(def))
	if err != nil {
		t.Fatalf("the definition did not marshal: %v", err)
	}
	var said map[string]any
	if err := json.Unmarshal(wire, &said); err != nil {
		t.Fatalf("the definition did not come back: %v", err)
	}
	return func(name string) (map[string]any, bool) {
		if name != def.Name {
			return nil, false
		}
		return said, true
	}
}

// A type exists because it was attested. Attesting one that already says
// exactly this mints a second definition identical to the first, and the pair
// of them are two claims where there was one.
func TestEnsureTypesLeavesATypeThatAlreadySaysThis(t *testing.T) {
	store := &MockAttestationStore{}

	if err := EnsureTypes(store, saying(t, PromptResult), "prompt-direct", PromptResult); err != nil {
		t.Fatalf("EnsureTypes returned %v", err)
	}

	if len(store.attestations) != 0 {
		t.Fatalf("a type that already says this was attested again: %d written", len(store.attestations))
	}
}

// A definition that changed is attested, and the newer claim supersedes the
// older the way any newer claim does.
func TestEnsureTypesAttestsATypeThatNowSaysSomethingElse(t *testing.T) {
	store := &MockAttestationStore{}

	changed := PromptResult
	changed.Color = "#000000"

	if err := EnsureTypes(store, saying(t, PromptResult), "prompt-direct", changed); err != nil {
		t.Fatalf("EnsureTypes returned %v", err)
	}

	if len(store.attestations) != 1 {
		t.Fatalf("a changed definition was written %d times, want 1", len(store.attestations))
	}
	if got := store.attestations[0].Attributes["display_color"]; got != "#000000" {
		t.Fatalf("the attested colour is %v, want the changed one", got)
	}
}

// A type nothing has said anything about is attested.
func TestEnsureTypesAttestsATypeNothingHasSaid(t *testing.T) {
	store := &MockAttestationStore{}

	if err := EnsureTypes(store, SaysNothing, "prompt-direct", PromptResult); err != nil {
		t.Fatalf("EnsureTypes returned %v", err)
	}

	if len(store.attestations) != 1 {
		t.Fatalf("a type nothing had said was written %d times, want 1", len(store.attestations))
	}
}

// A tag is a type nobody's code has an opinion about. Whatever it says now is
// what somebody said, and the thing that first used the tag saying it again
// would take that back.
func TestEnsureTypesExistLeavesATypeThatSaysSomethingElse(t *testing.T) {
	store := &MockAttestationStore{}

	chosen := PromptResult
	chosen.Color = "#123456"

	if err := EnsureTypesExist(store, saying(t, chosen), "tagging", PromptResult); err != nil {
		t.Fatalf("EnsureTypesExist returned %v", err)
	}

	if len(store.attestations) != 0 {
		t.Fatalf("a colour somebody chose was attested over: %d written", len(store.attestations))
	}
}

// A tag nothing has said anything about is attested, which is how it comes to
// exist at all.
func TestEnsureTypesExistAttestsATypeNothingHasSaid(t *testing.T) {
	store := &MockAttestationStore{}

	if err := EnsureTypesExist(store, SaysNothing, "tagging", PromptResult); err != nil {
		t.Fatalf("EnsureTypesExist returned %v", err)
	}

	if len(store.attestations) != 1 {
		t.Fatalf("a type nothing had said was written %d times, want 1", len(store.attestations))
	}
}
