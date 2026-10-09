package ats

// EntityResolver provides alternative identifier resolution for entities.
// Implementations can provide custom logic for finding alternative IDs
// from external systems (e.g., contact databases, identity stores).
type EntityResolver interface {
	// GetAlternativeIDs returns all alternative identifiers for the given ID.
	// This is used during query expansion to ensure all representations of
	// an entity are included in searches.
	// Returns empty slice if no alternatives found (not an error).
	GetAlternativeIDs(id string) ([]string, error)
}

// NoOpEntityResolver is a resolver that returns no alternative IDs.
// Use this for standalone ATS installations without external identity systems.
type NoOpEntityResolver struct{}

// GetAlternativeIDs returns empty slice (no external identity resolution).
func (n *NoOpEntityResolver) GetAlternativeIDs(id string) ([]string, error) {
	return []string{}, nil
}
