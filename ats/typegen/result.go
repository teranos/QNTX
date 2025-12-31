package typegen

// Result holds the generated types for all types in a package.
// This is language-agnostic - each Generator formats it differently.
type Result struct {
	// Types maps Go type names to their string representations
	// The string format depends on the Generator implementation
	Types map[string]string

	// PackageName is the Go package that was processed
	PackageName string

	// TypePositions maps type names to their source location
	// Used for generating documentation links
	TypePositions map[string]Position
}

// Position represents a source code location
type Position struct {
	// File is the repository-relative path (e.g., "ats/types/types.go")
	File string
	// Line is the line number where the type is defined
	Line int
}
