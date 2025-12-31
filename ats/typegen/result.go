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

	// Consts maps constant names to their values (for untyped consts)
	// e.g., const I = "⍟" → Consts["I"] = "⍟"
	Consts map[string]string

	// Arrays maps variable names to their slice literal values
	// e.g., var X = []string{"a", "b"} → Arrays["X"] = []string{"a", "b"}
	Arrays map[string][]string

	// Maps maps variable names to their map literal values
	// e.g., var X = map[string]string{"k": "v"} → Maps["X"] = map[string]string{"k": "v"}
	Maps map[string]map[string]string
}

// Position represents a source code location
type Position struct {
	// File is the repository-relative path (e.g., "ats/types/types.go")
	File string
	// Line is the line number where the type is defined
	Line int
}
