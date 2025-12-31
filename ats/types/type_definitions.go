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

// TypeDef defines a QNTX domain type with display metadata and semantic information.
// Types are richer than single predicates - they represent semantic categories with
// multiple identifying patterns, relationships, and behavioral rules.
type TypeDef struct {
	Name       string  // Type identifier (e.g., "commit", "author")
	Label      string  // Human-readable label for UI (e.g., "Commit", "Author")
	Color      string  // Hex color code for graph visualization (e.g., "#34495e")
	Opacity    float64 // Visual opacity (0.0-1.0), defaults to 1.0 if unset (zero value)
	Deprecated bool    // Whether this type is being phased out
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

// EnsureTypes ensures the specified types exist in the attestation store.
// This creates type attestations with display metadata for graph visualization.
//
// Non-fatal: If type creation fails, the error is returned but ingestion can continue
// with hardcoded fallback type colors/labels.
//
// Example usage:
//
//	err := types.EnsureTypes(store, "ixgest-git", types.Commit, types.Author, types.Branch)
//	if err != nil {
//	    logger.Errorw("Failed to create type definitions - falling back to hardcoded types",
//	        "error", err,
//	        "impact", "graph visualization may lack custom type metadata")
//	}
func EnsureTypes(store AttestationStore, source string, typeDefs ...TypeDef) error {
	var errors []error

	for _, def := range typeDefs {
		// Default opacity to 1.0 if not set
		opacity := def.Opacity
		if opacity == 0.0 {
			opacity = 1.0
		}

		attrs := map[string]interface{}{
			"display_color": def.Color,
			"display_label": def.Label,
			"deprecated":    def.Deprecated,
			"opacity":       opacity,
		}

		if err := AttestType(store, def.Name, source, attrs); err != nil {
			errors = append(errors, fmt.Errorf("failed to attest type %s: %w", def.Name, err))
		}
	}

	// Return combined error if any failed, but all were attempted
	if len(errors) > 0 {
		errMsg := "failed to create some type definitions:"
		for _, err := range errors {
			errMsg += "\n  - " + err.Error()
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}
