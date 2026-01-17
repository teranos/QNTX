package graph

import (
	"sort"

	"github.com/teranos/QNTX/ats/types"
)

// No longer needed - using types.TypeDef directly

// extractNodeTypes extracts explicit node_type attestations from the data.
// Returns a map of subject -> type for nodes that have explicit type declarations.
// This eliminates the need for fragile keyword-based inference.
func (b *AxGraphBuilder) extractNodeTypes(attestations []types.As) map[string]string {
	nodeTypes := make(map[string]string)

	for _, attestation := range attestations {
		claims := expandAttestation(attestation)
		for _, claim := range claims {
			// Look for "as <subject> node_type <type>" attestations
			if claim.Predicate == "node_type" && claim.Context != "" {
				nodeTypes[claim.Subject] = claim.Context
			}
		}
	}

	return nodeTypes
}

// extractTypeDefinitions extracts type definitions from attestations with display metadata.
// Looks for attestations with predicate "type" and context "graph" (graph visualization).
//
// Schema:
//
//	Subject: artist
//	Predicate: type (reserved)
//	Context: graph
//	Actors: ["artist"] (self-certifying in typespace)
//	Attributes: {"display_color": "#e74c3c", "display_label": "Artist", "deprecated": false}
//
// Type definitions exist in typespace (separate from ASID entity space) and are self-certifying.
// Each ixgest processor creates type definitions for its domain (ix-music, ix-weather, ix-git, etc.)
//
// Returns map of type name -> TypeDef (using ats/types.TypeDef directly)
func (b *AxGraphBuilder) extractTypeDefinitions(attestations []types.As) map[string]types.TypeDef {
	typeDefinitions := make(map[string]types.TypeDef)

	for _, attestation := range attestations {
		claims := expandAttestation(attestation)
		for _, claim := range claims {
			// Look for "as <type_name> type graph" attestations
			if claim.Predicate == "type" && claim.Context == "graph" {
				typeName := claim.Subject

				// Create TypeDef from attestation attributes
				opacity := 1.0 // Default full opacity
				def := types.TypeDef{
					Name:    typeName,
					Opacity: &opacity,
				}

				// Parse Attributes if present
				if attestation.Attributes != nil {
					if color, ok := attestation.Attributes["display_color"].(string); ok {
						def.Color = color
					}
					if label, ok := attestation.Attributes["display_label"].(string); ok {
						def.Label = label
					}
					if deprecated, ok := attestation.Attributes["deprecated"].(bool); ok {
						def.Deprecated = deprecated
					}
					if opacityVal, ok := attestation.Attributes["opacity"].(float64); ok {
						def.Opacity = &opacityVal
					}
					// Extract rich_string_fields array if present
					if richFields, ok := attestation.Attributes["rich_string_fields"].([]interface{}); ok {
						def.RichStringFields = make([]string, 0, len(richFields))
						for _, field := range richFields {
							if fieldStr, ok := field.(string); ok {
								def.RichStringFields = append(def.RichStringFields, fieldStr)
							}
						}
					}
					// Extract array_fields array if present
					if arrayFields, ok := attestation.Attributes["array_fields"].([]interface{}); ok {
						def.ArrayFields = make([]string, 0, len(arrayFields))
						for _, field := range arrayFields {
							if fieldStr, ok := field.(string); ok {
								def.ArrayFields = append(def.ArrayFields, fieldStr)
							}
						}
					}
				}

				// Later attestations override earlier ones (natural evolution)
				typeDefinitions[typeName] = def
			}
		}
	}

	return typeDefinitions
}

// determineNodeType resolves the semantic type for a node entity based on attestations.
// Node types are determined exclusively from explicit node_type attestations.
// If no type attestation exists, the node is marked as "untyped".
//
// This attestation-first approach ensures:
//   - No fragile heuristics or pattern matching
//   - No database-specific lookups or caching
//   - Type information is self-describing in the attestation data
//   - Portable across different QNTX deployments
func (b *AxGraphBuilder) determineNodeType(
	entity string,
	normalizedID string,
	predicate string,
	context string,
	nodeTypeMap map[string]string,
) (nodeType string, typeSource string) {
	// Check for explicit node_type attestation
	if explicitType, hasExplicitType := nodeTypeMap[entity]; hasExplicitType {
		return explicitType, "attested"
	}

	// No type information - leave untyped
	return "untyped", "untyped"
}

// collectNodeTypeInfo collects information about node types present in the graph.
// Uses only attested type definitions - no fallback maps or heuristics.
// Returns a list of node type metadata including count and color for each type.
func collectNodeTypeInfo(nodes []Node, typeDefinitions map[string]types.TypeDef) []NodeTypeInfo {
	// Count nodes by type
	typeCounts := make(map[string]int)
	for _, node := range nodes {
		typeCounts[node.Type]++
	}

	// Build node type info list
	var nodeTypes []NodeTypeInfo
	for nodeType, count := range typeCounts {
		var color, label string
		var richStringFields, arrayFields []string
		var opacity *float64
		var deprecated bool

		// Use attested type definition if available
		if typeDef, hasAttestedDef := typeDefinitions[nodeType]; hasAttestedDef {
			color = typeDef.Color
			label = typeDef.Label
			richStringFields = typeDef.RichStringFields
			arrayFields = typeDef.ArrayFields
			opacity = typeDef.Opacity
			deprecated = typeDef.Deprecated
		} else {
			// No attestation - use defaults for untyped
			color = defaultUntypedColor
			label = nodeType // Use raw type string as label
		}

		nodeTypes = append(nodeTypes, NodeTypeInfo{
			Type:             nodeType,
			Label:            label,
			Color:            color,
			Count:            count,
			RichStringFields: richStringFields,
			ArrayFields:      arrayFields,
			Opacity:          opacity,
			Deprecated:       deprecated,
		})
	}

	// Sort by count (descending) for better UX
	// Most common types appear first in frontend legend
	sort.Slice(nodeTypes, func(i, j int) bool {
		return nodeTypes[i].Count > nodeTypes[j].Count
	})

	return nodeTypes
}
