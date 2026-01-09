package typegen

// Result holds the generated types for all types in a package.
// This is language-agnostic - each Generator formats it differently.
type Result struct {
	// Types maps Go type names to their string representations
	// The string format depends on the Generator implementation
	Types map[string]string

	// PackageName is the Go package that was processed
	PackageName string

	// SourceFile is the repository-relative path to the package's source file
	// e.g., "sym/symbols.go" - used for documentation links
	SourceFile string

	// GitHubBaseURL is the base URL for linking to source files on GitHub
	// e.g., "https://github.com/teranos/QNTX/blob/main"
	// Detected from go.mod module path (github.com/owner/repo → https://github.com/owner/repo/blob/main)
	GitHubBaseURL string

	// TypePositions maps type names to their source location
	// Used for generating documentation links
	TypePositions map[string]Position

	// TypeComments maps type names to their Go doc comments
	// Used for preserving documentation in generated code
	TypeComments map[string]string

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

// IsConstReference checks if a string value is a const reference (exists in consts map)
func IsConstReference(value string, consts map[string]string) bool {
	_, isConst := consts[value]
	return isConst
}

// FormatMapEntry formats a map key or value for output.
// If the value is a const reference, applies constTransform; otherwise quotes it.
// This is the common pattern used by all generators for map formatting.
func FormatMapEntry(value string, consts map[string]string, constTransform func(string) string) string {
	if IsConstReference(value, consts) {
		return constTransform(value)
	}
	return "\"" + value + "\""
}
