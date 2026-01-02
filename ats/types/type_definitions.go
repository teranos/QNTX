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
	Name       string   // Type identifier (e.g., "commit", "author")
	Label      string   // Human-readable label for UI (e.g., "Commit", "Author")
	Color      string   // Hex color code for graph visualization (e.g., "#34495e")
	Opacity    *float64 // Visual opacity (0.0-1.0), nil defaults to 1.0
	Deprecated bool     // Whether this type is being phased out
}

// RelationshipTypeDef defines a relationship type with physics and display metadata.
// Relationship types represent predicates with their own visualization behavior,
// allowing domains to control how their relationships render in force-directed graphs.
type RelationshipTypeDef struct {
	Name         string   // Predicate name (e.g., "is_child_of", "points_to")
	Label        string   // Human-readable label for UI (e.g., "Child Of", "Points To")
	Color        string   // Optional link color override (hex code)
	LinkDistance *float64 // D3 force distance override (nil = use default)
	LinkStrength *float64 // D3 force strength override (nil = use default)
	Deprecated   bool     // Whether this relationship type is being phased out
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
		// Default opacity to 1.0 if not explicitly set
		opacity := 1.0
		if def.Opacity != nil {
			opacity = *def.Opacity
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

// AttestRelationshipType creates a relationship type definition attestation with physics metadata.
//
// Format: "as <predicateName> relationship_type graph" with self-certifying actor.
//
// Similar to node types, relationship types use the type-as-actor pattern in typespace.
// The predicate name becomes its own actor, avoiding bounded storage limits.
//
// Attributes typically include physics and display metadata for graph visualization:
//   - display_label: Human-readable label (e.g., "Child Of")
//   - link_distance: D3 force distance (e.g., 50)
//   - link_strength: D3 force strength (e.g., 0.3)
//   - color: Optional link color override (e.g., "#666")
//   - deprecated: Boolean flag for phasing out types
//
// Example usage:
//
//	attrs := map[string]interface{}{
//	    "display_label": "Child Of",
//	    "link_distance": 50.0,
//	    "link_strength": 0.3,
//	    "deprecated": false,
//	}
//	err := types.AttestRelationshipType(store, "is_child_of", "ix-git", attrs)
func AttestRelationshipType(store AttestationStore, predicateName, source string, attributes map[string]interface{}) error {
	if predicateName == "" {
		return fmt.Errorf("predicateName cannot be empty")
	}
	if source == "" {
		return fmt.Errorf("source cannot be empty")
	}

	// Generate ASID for the relationship type definition
	// Using "relationship_type" as predicate to distinguish from node types
	asid, err := id.GenerateASID(predicateName, "relationship_type", "graph", "")
	if err != nil {
		return fmt.Errorf("failed to generate ASID for relationship type %s: %w", predicateName, err)
	}

	// Create attestation with self-certifying actor
	attestation := &As{
		ID:         asid,
		Subjects:   []string{predicateName},
		Predicates: []string{"relationship_type"},
		Contexts:   []string{"graph"},
		Actors:     []string{predicateName}, // Self-certifying: predicate IS its own actor
		Timestamp:  time.Now(),
		Source:     source,
		Attributes: attributes,
	}

	// Store the attestation
	if err := store.CreateAttestation(attestation); err != nil {
		return fmt.Errorf("failed to create relationship type attestation for %s: %w", predicateName, err)
	}

	return nil
}

// EnsureRelationshipTypes ensures the specified relationship types exist in the attestation store.
// This creates relationship type attestations with physics and display metadata for graph visualization.
//
// Non-fatal: If relationship type creation fails, the error is returned but ingestion can continue
// with hardcoded fallback physics values in the frontend.
//
// Example usage:
//
//	err := types.EnsureRelationshipTypes(store, "ixgest-git", types.IsChildOf, types.PointsTo)
//	if err != nil {
//	    logger.Errorw("Failed to create relationship type definitions",
//	        "error", err,
//	        "impact", "graph physics will use default values")
//	}
func EnsureRelationshipTypes(store AttestationStore, source string, relationshipDefs ...RelationshipTypeDef) error {
	var errors []error

	for _, def := range relationshipDefs {
		attrs := map[string]interface{}{
			"display_label": def.Label,
			"deprecated":    def.Deprecated,
		}

		// Only include optional fields if set
		if def.Color != "" {
			attrs["color"] = def.Color
		}
		if def.LinkDistance != nil {
			attrs["link_distance"] = *def.LinkDistance
		}
		if def.LinkStrength != nil {
			attrs["link_strength"] = *def.LinkStrength
		}

		if err := AttestRelationshipType(store, def.Name, source, attrs); err != nil {
			errors = append(errors, fmt.Errorf("failed to attest relationship type %s: %w", def.Name, err))
		}
	}

	// Return combined error if any failed, but all were attempted
	if len(errors) > 0 {
		errMsg := "failed to create some relationship type definitions:"
		for _, err := range errors {
			errMsg += "\n  - " + err.Error()
		}
		return fmt.Errorf("%s", errMsg)
	}

	return nil
}
