package graph

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// buildGraphFromAttestations converts attestation data into a graph visualization structure.
// It expands each attestation into individual claims (subject-predicate-context triples),
// creates nodes for entities, and links between them. Node types are inferred from attestations,
// and link weights accumulate for repeated relationships.
func (b *AxGraphBuilder) buildGraphFromAttestations(attestations []types.As, query string) *Graph {
	graph := &Graph{
		Nodes: []Node{},
		Links: []Link{},
		Meta: Meta{
			GeneratedAt: time.Now(),
			Stats:       Stats{},
			Config: map[string]string{
				"query":       query,
				"description": fmt.Sprintf("Graph for query: %s", query),
			},
		},
	}

	// Extract explicit node types from attestations (subject -> type mapping)
	nodeTypeMap := b.extractNodeTypes(attestations)

	// Extract type definitions from attestations (type display metadata)
	typeDefinitions := b.extractTypeDefinitions(attestations)

	// Extract relationship type definitions from attestations (physics metadata)
	relationshipDefinitions := b.extractRelationshipTypeDefinitions(attestations)

	// Track unique nodes by ID
	nodeMap := make(map[string]*Node)
	linkMap := make(map[string]*Link)

	// Process each attestation
	for _, attestation := range attestations {
		// Expand attestation into individual claims
		claims := expandAttestation(attestation)

		for _, claim := range claims {
			// Create or update subject node
			subjectID := normalizeNodeID(claim.Subject)

			// Determine subject node type
			subjectNodeType, subjectTypeSource := b.determineNodeType(
				claim.Subject,
				subjectID,
				claim.Predicate,
				claim.Context,
				nodeTypeMap,
			)

			if _, exists := nodeMap[subjectID]; !exists {
				nodeMap[subjectID] = &Node{
					ID:         subjectID,
					Type:       subjectNodeType,
					TypeSource: subjectTypeSource,
					Label:      claim.Subject,
					Visible:    true,
					Group:      0, // Group assigned from type definitions
					Metadata: map[string]interface{}{
						"original_id": claim.Subject,
					},
				}
			}

			// Skip metadata predicates that shouldn't create visual links
			// node_type is metadata about the node itself, not a relationship
			if claim.Predicate == "node_type" {
				continue
			}

			// Create or update object node (if not a literal value)
			if !isLiteralValue(claim.Context) {
				objectID := normalizeNodeID(claim.Context)

				// Determine object node type
				objectNodeType, objectTypeSource := b.determineNodeType(
					claim.Context,
					objectID,
					claim.Predicate,
					claim.Context,
					nodeTypeMap,
				)

				if _, exists := nodeMap[objectID]; !exists {
					nodeMap[objectID] = &Node{
						ID:         objectID,
						Type:       objectNodeType,
						TypeSource: objectTypeSource,
						Label:      claim.Context,
						Visible:    true,
						Group:      0, // Group assigned from type definitions
						Metadata: map[string]interface{}{
							"original_id": claim.Context,
						},
					}
				}

				// Create link
				linkID := fmt.Sprintf("%s_%s_%s", subjectID, claim.Predicate, objectID)
				if _, exists := linkMap[linkID]; !exists {
					linkMap[linkID] = &Link{
						Source: subjectID,
						Target: objectID,
						Type:   claim.Predicate,
						Weight: defaultLinkWeight,
						Label:  claim.Predicate,
					}
				} else {
					// Increase weight for duplicate relationships
					linkMap[linkID].Weight += linkWeightIncrement
				}
			} else {
				// For literal values, add as metadata to the subject node
				node := nodeMap[subjectID]
				if node.Metadata == nil {
					node.Metadata = make(map[string]interface{})
				}
				node.Metadata[claim.Predicate] = claim.Context
			}
		}
	}

	// Convert maps to slices with deterministic ordering
	// Sort by ID for consistent output across runs
	nodeIDs := make([]string, 0, len(nodeMap))
	for id := range nodeMap {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)

	for _, id := range nodeIDs {
		graph.Nodes = append(graph.Nodes, *nodeMap[id])
	}

	// Sort links by composite key for deterministic output
	linkIDs := make([]string, 0, len(linkMap))
	for id := range linkMap {
		linkIDs = append(linkIDs, id)
	}
	sort.Strings(linkIDs)

	for _, id := range linkIDs {
		graph.Links = append(graph.Links, *linkMap[id])
	}

	// Update meta
	graph.Meta.Stats.TotalNodes = len(graph.Nodes)
	graph.Meta.Stats.TotalEdges = len(graph.Links)

	// Collect node type information for frontend
	graph.Meta.NodeTypes = collectNodeTypeInfo(graph.Nodes, typeDefinitions)

	// Collect relationship type information for frontend (physics metadata)
	graph.Meta.RelationshipTypes = collectRelationshipTypeInfo(graph.Links, relationshipDefinitions)

	return graph
}

// Claim represents an individual subject-predicate-context claim
type Claim struct {
	Subject   string
	Predicate string
	Context   string
	Actor     string
	Timestamp string
}

// expandAttestation expands a compact attestation into individual subject-predicate-context claims.
// An attestation with N subjects, M predicates, and P contexts expands to N×M×P claims.
// This allows graph building from sparse attestation data where one record represents
// multiple relationships (e.g., a candidate with multiple skills at multiple companies).
func expandAttestation(attestation types.As) []Claim {
	// Preallocate slice with known capacity for better performance
	capacity := len(attestation.Subjects) * len(attestation.Predicates) * len(attestation.Contexts)
	claims := make([]Claim, 0, capacity)

	// For each combination of subject, predicate, context
	for _, subject := range attestation.Subjects {
		for _, predicate := range attestation.Predicates {
			for _, context := range attestation.Contexts {
				claims = append(claims, Claim{
					Subject:   subject,
					Predicate: predicate,
					Context:   context,
					Actor:     strings.Join(attestation.Actors, ", "),
					Timestamp: attestation.Timestamp.Format("2006-01-02 15:04:05"),
				})
			}
		}
	}

	return claims
}
