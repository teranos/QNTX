package types

import (
	atstypes "github.com/teranos/QNTX/ats/types"
)

// Helper function to create float64 pointers for optional fields
func floatPtr(f float64) *float64 {
	return &f
}

// Git entity types
var (
	Commit = atstypes.TypeDef{
		Name:  "commit",
		Label: "Commit",
		Color: "#34495e",
	}

	Author = atstypes.TypeDef{
		Name:  "author",
		Label: "Author",
		Color: "#c0392b",
	}

	Branch = atstypes.TypeDef{
		Name:  "branch",
		Label: "Branch",
		Color: "#16a085",
	}
)

// Git relationship types with physics metadata
var (
	// IsChildOf represents parent-child commit lineage
	// Tighter distance and weaker strength for flexible commit history layout
	IsChildOf = atstypes.RelationshipTypeDef{
		Name:         "is_child_of",
		Label:        "Child Of",
		LinkDistance: floatPtr(50),  // Shorter distance for tight lineage
		LinkStrength: floatPtr(0.3), // Weaker strength for flexibility
	}

	// PointsTo represents branch pointers to commits
	// Slightly longer distance and weaker strength for branch separation
	PointsTo = atstypes.RelationshipTypeDef{
		Name:         "points_to",
		Label:        "Points To",
		LinkDistance: floatPtr(60),  // Medium distance for branch clarity
		LinkStrength: floatPtr(0.2), // Weak strength for loose connections
	}
)
