package graph

const (
	// Link weight constants
	defaultLinkWeight   = 1.0 // Initial weight for new links
	linkWeightIncrement = 0.5 // Weight increase for duplicate relationships

	// Default color/label for untyped nodes (no attestation)
	defaultUntypedColor = "rgba(149, 165, 166, 0.3)" // Transparent gray
	defaultUntypedLabel = "Untyped"
)
