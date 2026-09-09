package types

import (
	"bytes"
	"encoding/json"
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
//	attrs := map[string]any{
//	    "display_color": "#e67e22",
//	    "display_label": "Document",
//	}
//	err := types.AttestType(store, "document", "ix-content", attrs)
func AttestType(store AttestationStore, typeName, source string, attributes map[string]any) error {
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

// sameAttributes reports whether a type says the same thing twice.
//
// Compared as the JSON they are stored and read back as, because that is the
// only form both sides share: what a store hands back has been through JSON,
// where a []string is a []any and 1.0 is 1, and neither survives a comparison
// of Go values with what a TypeDef just built.
func sameAttributes(said, wanted map[string]any) bool {
	saidJSON, err := json.Marshal(said)
	if err != nil {
		return false
	}
	wantedJSON, err := json.Marshal(wanted)
	if err != nil {
		return false
	}
	return bytes.Equal(saidJSON, wantedJSON)
}

// Says answers what a type says now, and false when it says nothing yet.
//
// A function rather than a method on AttestationStore: reading attestations
// takes a filter, the filter lives in package ats, and ats is what imports this
// one. What a type says is the whole of what EnsureTypes needs to know.
type Says func(typeName string) (map[string]any, bool)

// SaysNothing is what to pass when nothing has been read: every type is
// attested, which is what this did before it could ask.
func SaysNothing(string) (map[string]any, bool) { return nil, false }

// EnsureTypes attests the types that are not already attested as they are.
//
// A type exists because it was attested, so attesting one that already says
// exactly this says nothing: it mints a second definition identical to the
// first, and the pair of them are two claims where there was one. A type whose
// definition has changed is attested, and the newer claim supersedes the older
// the way any newer claim does.
//
// Non-fatal: If type creation fails, the error is returned but ingestion can continue
// with hardcoded fallback type colors/labels.
//
// Example usage:
//
//	err := types.EnsureTypes(store, says, "prompt", types.PromptResult, types.ClusterLabeled)
func EnsureTypes(store AttestationStore, says Says, source string, typeDefs ...TypeDef) error {
	var errs []error

	for _, def := range typeDefs {
		// Default opacity to 1.0 if not explicitly set
		if def.Opacity == nil {
			defaultOpacity := 1.0
			def.Opacity = &defaultOpacity
		}

		wanted := attrs.From(def)
		if said, ok := says(def.Name); ok && sameAttributes(said, wanted) {
			continue
		}

		if err := AttestType(store, def.Name, source, wanted); err != nil {
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
