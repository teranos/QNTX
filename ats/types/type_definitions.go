package types

import (
	"fmt"
	"time"

	"github.com/teranos/vanity-id"
)

// AttestationStore defines the minimal storage interface needed for type attestations.
// This avoids circular dependencies with the ats package.
type AttestationStore interface {
	CreateAttestation(as *As) error
}

// AttestType creates a type definition attestation with arbitrary attributes.
//
// Format: "as <typeName> type graph" with self-certifying actor (type-as-actor pattern).
//
// The typeName becomes its own actor in the typespace, separate from the ASID entity space.
// This avoids bounded storage limits (64 actors per entity) since each type self-certifies.
//
// Attributes typically include display metadata for graph visualization:
//   - display_color: Hex color code (e.g., "#3498db")
//   - display_label: Human-readable label (e.g., "Job Description")
//   - deprecated: Boolean flag for phasing out types
//   - opacity: Float for visual emphasis (0.0-1.0)
//
// But can contain any JSON-serializable data relevant to the type definition.
//
// Example usage:
//
//	attrs := map[string]interface{}{
//	    "display_color": "#e67e22",
//	    "display_label": "Job Description",
//	    "deprecated": false,
//	    "opacity": 1.0,
//	}
//	err := types.AttestType(store, "jd", "ix-jd", attrs)
func AttestType(store AttestationStore, typeName, source string, attributes map[string]interface{}) error {
	if typeName == "" {
		return fmt.Errorf("typeName cannot be empty")
	}
	if source == "" {
		return fmt.Errorf("source cannot be empty")
	}

	// Generate ASID for the type definition
	// Empty actor seed creates self-certifying ASID
	asid, err := id.GenerateASID(typeName, "type", "graph", "")
	if err != nil {
		return fmt.Errorf("failed to generate ASID for type %s: %w", typeName, err)
	}

	// Create attestation with self-certifying actor
	// Actor IS the type name itself (type-as-actor in typespace)
	attestation := &As{
		ID:         asid,
		Subjects:   []string{typeName},
		Predicates: []string{"type"},
		Contexts:   []string{"graph"},
		Actors:     []string{typeName}, // Self-certifying: type IS its own actor
		Timestamp:  time.Now(),
		Source:     source,
		Attributes: attributes,
	}

	// Store the attestation
	if err := store.CreateAttestation(attestation); err != nil {
		return fmt.Errorf("failed to create type attestation for %s: %w", typeName, err)
	}

	return nil
}
