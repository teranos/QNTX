package graph

import (
	"sort"

	"github.com/teranos/QNTX/ats/types"
)

// RelationshipDefinition holds physics and display metadata for a relationship type from attestations.
// Relationship type definitions use the "relationship_type" predicate in typespace.
type RelationshipDefinition struct {
	PredicateName string   `json:"predicate_name"`            // e.g., "is_child_of", "points_to"
	DisplayLabel  string   `json:"display_label"`             // Human-readable label
	Color         string   `json:"color,omitempty"`           // Optional link color override
	LinkDistance  *float64 `json:"link_distance,omitempty"`   // D3 force distance (nil = use default)
	LinkStrength  *float64 `json:"link_strength,omitempty"`   // D3 force strength (nil = use default)
	Deprecated    bool     `json:"deprecated"`                // Whether this relationship type is deprecated
}

// extractRelationshipTypeDefinitions extracts relationship type definitions from attestations with physics metadata.
// Looks for attestations with predicate "relationship_type" and context "graph".
//
// Schema:
//
//	Subject: is_child_of
//	Predicate: relationship_type (reserved)
//	Context: graph
//	Actors: ["is_child_of"] (self-certifying in typespace)
//	Attributes: {
//	    "display_label": "Child Of",
//	    "link_distance": 50,
//	    "link_strength": 0.3,
//	    "deprecated": false
//	}
//
// Relationship definitions exist in typespace (separate from ASID entity space) and are self-certifying.
// Each ixgest processor creates relationship type definitions for its domain (ix-git, ix-music, etc.)
//
// Returns map of predicate name -> RelationshipDefinition
func (b *AxGraphBuilder) extractRelationshipTypeDefinitions(attestations []types.As) map[string]RelationshipDefinition {
	relationshipDefinitions := make(map[string]RelationshipDefinition)

	for _, attestation := range attestations {
		claims := expandAttestation(attestation)
		for _, claim := range claims {
			// Look for "as <predicate_name> relationship_type graph" attestations
			if claim.Predicate == "relationship_type" && claim.Context == "graph" {
				predicateName := claim.Subject

				// Extract physics and display metadata from Attributes
				def := RelationshipDefinition{
					PredicateName: predicateName,
				}

				// Parse Attributes if present
				if attestation.Attributes != nil {
					if label, ok := attestation.Attributes["display_label"].(string); ok {
						def.DisplayLabel = label
					}
					if color, ok := attestation.Attributes["color"].(string); ok {
						def.Color = color
					}
					if distance, ok := attestation.Attributes["link_distance"].(float64); ok {
						def.LinkDistance = &distance
					}
					if strength, ok := attestation.Attributes["link_strength"].(float64); ok {
						def.LinkStrength = &strength
					}
					if deprecated, ok := attestation.Attributes["deprecated"].(bool); ok {
						def.Deprecated = deprecated
					}
				}

				// Later attestations override earlier ones (natural evolution)
				relationshipDefinitions[predicateName] = def
			}
		}
	}

	return relationshipDefinitions
}

// collectRelationshipTypeInfo collects information about relationship types present in the graph.
// Uses only attested relationship type definitions - no fallback maps or heuristics.
// Returns a list of relationship type metadata including count and physics for each type.
func collectRelationshipTypeInfo(links []Link, relationshipDefinitions map[string]RelationshipDefinition) []RelationshipTypeInfo {
	// Count links by type
	typeCounts := make(map[string]int)
	for _, link := range links {
		typeCounts[link.Type]++
	}

	// Build relationship type info list
	var relationshipTypes []RelationshipTypeInfo
	for linkType, count := range typeCounts {
		info := RelationshipTypeInfo{
			Type:  linkType,
			Label: linkType, // Default to type name if no definition
			Count: count,
		}

		// Use attested relationship type definition if available
		if relDef, hasAttestedDef := relationshipDefinitions[linkType]; hasAttestedDef {
			info.Label = relDef.DisplayLabel
			info.Color = relDef.Color
			info.LinkDistance = relDef.LinkDistance
			info.LinkStrength = relDef.LinkStrength
		}

		relationshipTypes = append(relationshipTypes, info)
	}

	// Sort by count (descending) for better UX
	// Most common relationship types appear first
	sort.Slice(relationshipTypes, func(i, j int) bool {
		return relationshipTypes[i].Count > relationshipTypes[j].Count
	})

	return relationshipTypes
}
