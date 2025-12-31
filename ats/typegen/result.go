package typegen

// Result holds the generated types for all types in a package.
// This is language-agnostic - each Generator formats it differently.
type Result struct {
	// Types maps Go type names to their string representations
	// The string format depends on the Generator implementation
	Types map[string]string

	// PackageName is the Go package that was processed
	PackageName string
}
