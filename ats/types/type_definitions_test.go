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
