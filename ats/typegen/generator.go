package typegen

// Generator defines the interface for language-specific type generators.
// Each target language (TypeScript, Python, Rust, Dart) implements this interface.
type Generator interface {
	// GenerateFile creates a complete output file from parsed Go types
	GenerateFile(result *Result) string

	// FileExtension returns the file extension for this language (e.g., "ts", "py", "rs")
	FileExtension() string

	// Language returns the language name (e.g., "typescript", "python")
	Language() string
}
