package graph

import (
	"time"
)

// Graph represents the complete graph structure for visualization
type Graph struct {
	Nodes []Node `json:"nodes"`
	Links []Link `json:"links"`
	Meta  Meta   `json:"meta"`
}

// Node represents an entity in the graph
type Node struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`            // Node type from attestations ("artist", "album", "genre") or "untyped"
	TypeSource string                 `json:"-"`               // Internal only: "attested" or "untyped"
	Label      string                 `json:"label"`           // Display label
	Visible    bool                   `json:"visible"`         // Backend controls visibility
	Group      int                    `json:"group,omitempty"` // For coloring/clustering (from type definitions)
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Link represents a relationship between nodes
type Link struct {
	Source string  `json:"source"` // Node ID
	Target string  `json:"target"` // Node ID
	Type   string  `json:"type"`   // Predicate from attestation (e.g., "performed_by", "released_on", "genre_of")
	Weight float64 `json:"value"`  // Link strength/weight (D3 uses "value")
	Label  string  `json:"label,omitempty"`
	Hidden bool    `json:"hidden,omitempty"` // Phase 2: Server-controlled visibility
}

// Meta contains metadata about the graph
type Meta struct {
	GeneratedAt       time.Time              `json:"generated_at"`
	Stats             Stats                  `json:"stats"`
	Config            map[string]string      `json:"config"`
	NodeTypes         []NodeTypeInfo         `json:"node_types"`         // Available node types in this graph
	RelationshipTypes []RelationshipTypeInfo `json:"relationship_types"` // Available relationship types with physics
}

// NodeTypeInfo describes a node type and its visual configuration
// Embeds TypeDef to avoid duplication of type metadata
type NodeTypeInfo struct {
	Type  string `json:"type"`            // e.g., "artist", "album", "genre"
	Label string `json:"label"`           // Human-readable display name (e.g., "Artist", "Album")
	Color string `json:"color,omitempty"` // Hex color code
	Count int    `json:"count,omitempty"` // Number of nodes of this type
	// Fields from TypeDef
	RichStringFields []string `json:"rich_string_fields,omitempty"` // Metadata fields for semantic search
	ArrayFields      []string `json:"array_fields,omitempty"`       // Fields flattened into arrays
	Opacity          *float64 `json:"opacity,omitempty"`            // Visual opacity
	Deprecated       bool     `json:"deprecated,omitempty"`         // Whether this type is being phased out
}

// RelationshipTypeInfo describes a relationship type with physics and visual configuration
type RelationshipTypeInfo struct {
	Type         string   `json:"type"`                    // Predicate name (e.g., "is_child_of", "points_to")
	Label        string   `json:"label"`                   // Human-readable display name
	Color        string   `json:"color,omitempty"`         // Optional link color override
	LinkDistance *float64 `json:"link_distance,omitempty"` // D3 force distance override (nil = use default)
	LinkStrength *float64 `json:"link_strength,omitempty"` // D3 force strength override (nil = use default)
	Count        int      `json:"count,omitempty"`         // Number of links of this type
}

// Stats provides graph statistics
type Stats struct {
	TotalNodes int `json:"total_nodes,omitempty"`
	TotalEdges int `json:"total_edges,omitempty"`
}
