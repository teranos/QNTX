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
	Type       string                 `json:"type"`    // Node type from attestations ("artist", "album", "genre") or "untyped"
	TypeSource string                 `json:"-"`       // Internal only: "attested" or "untyped"
	Label      string                 `json:"label"`   // Display label
	Visible    bool                   `json:"visible"` // Backend controls visibility
	Group      int                    `json:"group"`   // For coloring/clustering (from type definitions)
	Metadata   map[string]interface{} `json:"metadata"`
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
	GeneratedAt time.Time         `json:"generated_at"`
	Stats       Stats             `json:"stats"`
	Config      map[string]string `json:"config"`
	NodeTypes   []NodeTypeInfo    `json:"node_types"` // Available node types in this graph
}

// NodeTypeInfo describes a node type and its visual configuration
type NodeTypeInfo struct {
	Type  string `json:"type"`  // e.g., "artist", "album", "genre"
	Label string `json:"label"` // Human-readable display name (e.g., "Artist", "Album")
	Color string `json:"color"` // Hex color code
	Count int    `json:"count"` // Number of nodes of this type
}

// Stats provides graph statistics
type Stats struct {
	TotalNodes int `json:"total_nodes"`
	TotalEdges int `json:"total_edges"`
}
