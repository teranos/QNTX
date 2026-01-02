package types

import "testing"

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

	err := EnsureTypes(store, "test-source", typeDefs...)
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
