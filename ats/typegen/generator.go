// Package typegen generates type definitions from Go source code for multiple target languages.
//
// # Architecture
//
// The package uses a two-layer design:
//  1. Language-agnostic parsing (typegen.go) extracts Go types into a generic Result
//  2. Language-specific generators (typescript/, markdown/, etc.) format the Result
//
// This separation allows adding new target languages without duplicating AST traversal logic.
//
// # Design Decisions
//
// - Two-pass import resolution: First pass generates all types, second pass adds cross-package
//   imports. This prevents circular dependencies and ensures deterministic output.
// - Generators implement a common interface for extensibility
// - Deterministic output (sorted maps) enables CI validation via git diff
// - Excluded types list prevents generation of internal implementation details
//
// # Implementing a New Generator
//
// To add support for a new language (e.g., Python):
//
//  1. Create package: ats/typegen/python/generator.go
//  2. Implement the Generator interface (see below)
//  3. Add language to getLanguages() in cmd/qntx/commands/typegen.go
//  4. Add integration tests in ats/typegen/integration_test.go
//  5. Update docs/typegen.md with language-specific struct tags if needed
//  6. Add file extension to getOutputConfig() in cmd/qntx/commands/typegen.go
//
// Example:
//
//	type PythonGenerator struct{}
//
//	func (g *PythonGenerator) Language() string { return "python" }
//	func (g *PythonGenerator) FileExtension() string { return "py" }
//	func (g *PythonGenerator) GenerateInterface(name string, structType *ast.StructType) string {
//	    // Convert Go struct to Python dataclass or Pydantic model
//	}
//	// ... implement other methods
package typegen

import "go/ast"

// Generator defines the interface for language-specific type generators.
// Each target language (TypeScript, Python, Rust, Dart) implements this interface.
type Generator interface {
	// GenerateFile creates a complete output file from parsed Go types
	GenerateFile(result *Result) string

	// FileExtension returns the file extension for this language (e.g., "ts", "py", "rs")
	FileExtension() string

	// Language returns the language name (e.g., "typescript", "python")
	Language() string

	// GenerateInterface converts a Go struct to a language-specific interface/class
	GenerateInterface(name string, structType *ast.StructType) string

	// GenerateUnionType converts a set of const values to a language-specific union/enum
	GenerateUnionType(name string, values []string) string
}
