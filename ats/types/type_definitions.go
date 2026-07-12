package types

import (
	"time"

	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/errors"
)

// PromptResult is the type for LLM prompt execution results.
// Created by Prompt glyphs after successful execution, making responses
// discoverable in the attestation graph.
var PromptResult = TypeDef{
	Name:             "prompt-result",
	Label:            "Prompt Result",
	Color:            "#9b59b6",
	RichStringFields: []string{"response"},
}

// ClusterLabeled is the type for LLM-generated cluster labels.
// Created by the cluster labeling Pulse job (qntx@embeddings actor).
var ClusterLabeled = TypeDef{
	Name:             "labeled",
	Label:            "Cluster Label",
	Color:            "#60a5fa",
	RichStringFields: []string{"label"},
}

// AttestationStore defines the minimal storage interface needed for type attestations.
// This avoids circular dependencies with the ats package.
type AttestationStore interface {
	CreateAttestation(as *As) error
}

// TypeDef defines a QNTX domain type with display metadata and semantic information.
// Types are richer than single predicates - they represent semantic categories with
// multiple identifying patterns, relationships, and behavioral rules.
type TypeDef struct {
	Name             string   `json:"name"`                                                             // Type identifier (e.g., "commit", "author")
	Label            string   `json:"label" attr:"display_label"`                                       // Human-readable label for UI (e.g., "Commit", "Author")
	Color            string   `json:"color" attr:"display_color"`                                       // Hex color code for graph visualization (e.g., "#34495e")
	Opacity          *float64 `json:"opacity,omitempty" attr:"opacity,omitempty"`                       // Visual opacity (0.0-1.0), nil defaults to 1.0
	Deprecated       bool     `json:"deprecated" attr:"deprecated"`                                     // Whether this type is being phased out
	RichStringFields []string `json:"rich_string_fields,omitempty" attr:"rich_string_fields,omitempty"` // Metadata field names containing rich text for semantic search (e.g., ["notes", "description"])
	ArrayFields      []string `json:"array_fields,omitempty" attr:"array_fields,omitempty"`             // Field names that should be flattened into arrays (e.g., ["skills", "languages", "certifications"])
}

// AttestType creates a type definition attestation with arbitrary attributes.
//
// Format: "[typeName] is type" with self-certifying actor (type-as-actor pattern).
// No context — a type exists because it was attested, not because it belongs to a namespace.
//
// The typeName becomes its own actor in the typespace, separate from the ASID entity space.
// This avoids bounded storage limits (64 actors per entity) since each type self-certifies.
//
// Example usage:
//
//	attrs := map[string]interface{}{
//	    "display_color": "#e67e22",
//	    "display_label": "Document",
//	}
//	err := types.AttestType(store, "document", "ix-content", attrs)
func AttestType(store AttestationStore, typeName, source string, attributes map[string]interface{}) error {
	if typeName == "" {
		return errors.New("typeName cannot be empty")
	}
	if source == "" {
		return errors.New("source cannot be empty")
	}

	// Generate ASUID via Rust WASM engine
	asuid, err := identity.GenerateTypeID(typeName)
	if err != nil {
		return errors.Wrapf(err, "failed to generate type ID for %s", typeName)
	}

	// Create attestation with self-certifying actor
	// Actor IS the type name itself (type-as-actor in typespace)
	now := time.Now()
	attestation := &As{
		ID:         asuid,
		Subjects:   []string{typeName},
		Predicates: []string{"type"},
		Actors:     []string{typeName}, // Self-certifying: type IS its own actor
		Timestamp:  now,
		CreatedAt:  now,
		Source:     source,
		Attributes: attributes,
	}

	// Store the attestation
	if err := store.CreateAttestation(attestation); err != nil {
		return errors.Wrapf(err, "failed to create type attestation for %s", typeName)
	}

	return nil
}

// EnsureTypes ensures the specified types exist in the attestation store.
//
// Non-fatal: If type creation fails, the error is returned but ingestion can continue
// with hardcoded fallback type colors/labels.
//
// Example usage:
//
//	err := types.EnsureTypes(store, "ixgest-git", types.Commit, types.Author, types.Branch)
func EnsureTypes(store AttestationStore, source string, typeDefs ...TypeDef) error {
	var errs []error

	for _, def := range typeDefs {
		// Default opacity to 1.0 if not explicitly set
		if def.Opacity == nil {
			defaultOpacity := 1.0
			def.Opacity = &defaultOpacity
		}

		if err := AttestType(store, def.Name, source, attrs.From(def)); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to attest type %s", def.Name))
		}
	}

	// Return combined error if any failed, but all were attempted
	if len(errs) > 0 {
		errMsg := "failed to create some type definitions:"
		for _, err := range errs {
			errMsg += "\n  - " + err.Error()
		}
		return errors.New(errMsg)
	}

	return nil
}
