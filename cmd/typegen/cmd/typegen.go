package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/typegen"
	"github.com/teranos/QNTX/typegen/api"
	"github.com/teranos/QNTX/typegen/css"
	"github.com/teranos/QNTX/typegen/markdown"
	"github.com/teranos/QNTX/typegen/python"
	"github.com/teranos/QNTX/typegen/rust"
	"github.com/teranos/QNTX/typegen/typescript"
)

// Package lists per language target
// Each language can have a different set of packages to generate types from
var languagePackages = map[string][]string{
	"typescript": {
		"github.com/teranos/QNTX/ats/types",
		"github.com/teranos/QNTX/qntx-code/ast",        // AST transformation types
		"github.com/teranos/QNTX/qntx-code/vcs/github", // GitHub PR integration types
		"github.com/teranos/QNTX/qntx-code/ixgest/git", // Git ingestion result types
		"github.com/teranos/QNTX/graph",                // Frontend graph visualization
		"github.com/teranos/QNTX/pulse/async",
		"github.com/teranos/QNTX/pulse/budget",
		"github.com/teranos/QNTX/pulse/schedule",
		"github.com/teranos/QNTX/server",
		"github.com/teranos/QNTX/server/syscap", // System capabilities types
		"github.com/teranos/QNTX/sym",
	},
	"rust": {
		"github.com/teranos/QNTX/ats/types",
		// graph package excluded - frontend-only types
		"github.com/teranos/QNTX/pulse/async",
		"github.com/teranos/QNTX/pulse/budget",
		"github.com/teranos/QNTX/pulse/schedule",
		// server package excluded - server-side HTTP handler types
		"github.com/teranos/QNTX/server/syscap", // System capabilities types
		"github.com/teranos/QNTX/sym",
	},
	"python": {
		"github.com/teranos/QNTX/graph",
		"github.com/teranos/QNTX/sym",
	},
	"css": {
		"github.com/teranos/QNTX/sym",
	},
	"markdown": {
		"github.com/teranos/QNTX/ats/types",
		"github.com/teranos/QNTX/qntx-code/vcs/github",
		"github.com/teranos/QNTX/qntx-code/ixgest/git", // Git ingestion result types
		"github.com/teranos/QNTX/graph",
		"github.com/teranos/QNTX/pulse/async",
		"github.com/teranos/QNTX/pulse/budget",
		"github.com/teranos/QNTX/pulse/schedule",
		"github.com/teranos/QNTX/server",
		"github.com/teranos/QNTX/server/syscap", // System capabilities types
		"github.com/teranos/QNTX/sym",
	},
}

// Default packages when --packages flag is used (all packages for that language)
func getDefaultPackages(lang string) []string {
	if pkgs, ok := languagePackages[lang]; ok {
		return pkgs
	}
	// Fallback to TypeScript packages if language not defined
	return languagePackages["typescript"]
}

var (
	typegenOutput   string
	typegenPackages []string
	typegenLang     string
)

// TypegenCmd represents the typegen command
var TypegenCmd = &cobra.Command{
	Use:   "typegen",
	Short: "Generate type definitions from Go source",
	Long: `Generate type definitions from Go structs for multiple target languages.

This command parses Go source code and generates corresponding type definitions.
Supported languages: TypeScript, Python, Rust, Markdown (coming: Dart)

It handles:
  - Struct types → interfaces/classes
  - Type aliases with consts → union types/enums
  - JSON tags for field naming
  - Pointer types as optional fields
  - time.Time as string
  - map[string]interface{} as Record/dict/HashMap
  - API endpoint documentation (generated with markdown to docs/api/)

Examples:
  typegen                                    # Generate TypeScript to stdout
  typegen --lang typescript                  # Explicit language
  typegen --lang markdown                    # Generate docs/types/ and docs/api/
  typegen --lang all                         # All languages
  typegen --output types/generated/          # Write to directory (creates typescript/ subdir)
  typegen --packages pulse/async             # Specific package only`,
	RunE: runTypegen,
}

func init() {
	TypegenCmd.Flags().StringVarP(&typegenOutput, "output", "o", "", "Output directory (default: stdout)")
	TypegenCmd.Flags().StringSliceVarP(&typegenPackages, "packages", "p", nil, "Packages to process (default: ats/types, pulse/async, server)")
	TypegenCmd.Flags().StringVarP(&typegenLang, "lang", "l", "typescript", "Target language: typescript, python, rust, dart, all")

	// Add check subcommand
	TypegenCmd.AddCommand(TypegenCheckCmd)
}

// TypegenCheckCmd checks if generated types are up to date
var TypegenCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check if generated types are up to date",
	Long: `Check if generated types match the current Go source code.

This command generates types to a temporary directory and compares them
with existing types, ignoring metadata comments that change on every run.

Exit codes:
  0 - Types are up to date
  1 - Types are out of date (diff shown)
  2 - Error during check

Examples:
  typegen check                      # Check all generated types
  make types-check                   # Same, via Makefile`,
	RunE: runTypegenCheck,
}

func runTypegenCheck(cmd *cobra.Command, args []string) error {
	fmt.Println("Checking generated types...")

	// Create temp directory for generated types
	tempDir, err := os.MkdirTemp("", "qntx-types-check-*")
	if err != nil {
		return errors.Wrap(err, "failed to create temp directory")
	}
	defer os.RemoveAll(tempDir)

	// Override output directory for generation
	originalOutput := typegenOutput
	typegenOutput = tempDir
	defer func() { typegenOutput = originalOutput }()

	// Generate all types to temp directory
	languages := []string{"typescript", "python", "rust", "css", "markdown"}

	for _, lang := range languages {
		packages := getDefaultPackages(lang)
		if err := generateForLanguage(lang, packages, true); err != nil {
			return errors.Wrapf(err, "failed to generate %s types", lang)
		}
	}

	// Compare with existing types using the typegen package
	result, err := typegen.CompareDirectories(tempDir)
	if err != nil {
		return errors.Wrap(err, "failed to compare directories")
	}

	if result.UpToDate {
		fmt.Println("✓ Types are up to date")
		return nil
	}

	// Show differences
	fmt.Println("✗ Types are out of date.")
	for lang, files := range result.Differences {
		if len(files) > 0 {
			fmt.Printf("\n%s files differ:\n", lang)
			for _, file := range files {
				fmt.Printf("  - %s\n", file)
			}
		}
	}

	return errors.New("types are out of date - run 'make types' to update")
}

func runTypegen(cmd *cobra.Command, args []string) error {
	// Validate and determine languages to generate
	languages := getLanguages(typegenLang)
	if len(languages) == 0 {
		return errors.Newf("invalid language: %s (supported: typescript, python, rust, dart, all)", typegenLang)
	}

	// Generate for each language
	for _, lang := range languages {
		// Use custom packages if provided, otherwise use language-specific defaults
		packages := typegenPackages
		usingDefaultPackages := len(packages) == 0
		if usingDefaultPackages {
			packages = getDefaultPackages(lang)
		} else {
			// Expand short package names to full import paths
			packages = normalizePackagePaths(packages)
		}

		if err := generateForLanguage(lang, packages, usingDefaultPackages); err != nil {
			return errors.Wrapf(err, "failed to generate %s types", lang)
		}
	}

	return nil
}

// getLanguages returns the list of languages to generate based on the --lang flag
func getLanguages(lang string) []string {
	lang = strings.ToLower(strings.TrimSpace(lang))

	switch lang {
	case "all":
		return []string{"typescript", "python", "rust", "css", "markdown"} // All supported languages
	case "typescript", "ts":
		return []string{"typescript"}
	case "markdown", "md":
		return []string{"markdown"}
	case "python", "py":
		return []string{"python"}
	case "rust", "rs":
		return []string{"rust"}
	case "css":
		return []string{"css"}
	case "dart":
		return []string{"dart"}
	default:
		return nil
	}
}

// normalizePackagePaths expands short package names to full import paths
func normalizePackagePaths(packages []string) []string {
	normalized := make([]string, len(packages))
	for i, pkg := range packages {
		if !strings.Contains(pkg, "/") {
			// Short name like "types" -> "github.com/teranos/QNTX/types"
			normalized[i] = "github.com/teranos/QNTX/" + pkg
		} else if !strings.HasPrefix(pkg, "github.com/") {
			// Partial path like "ats/types" -> "github.com/teranos/QNTX/ats/types"
			normalized[i] = "github.com/teranos/QNTX/" + pkg
		} else {
			// Already full path
			normalized[i] = pkg
		}
	}
	return normalized
}

// genResult holds the generated output for a single package
type genResult struct {
	packageName string
	output      string
	typeNames   []string
	constNames  []string
	arrayNames  []string
	mapNames    []string
}

// generateForLanguage generates types for a specific language
func generateForLanguage(lang string, packages []string, generateIndex bool) error {
	// Determine output directory for this language
	outputDir, fileExt := getOutputConfig(lang)

	// Create the appropriate generator
	var gen typegen.Generator
	switch lang {
	case "typescript":
		gen = typescript.NewGenerator()
	case "markdown":
		gen = markdown.NewGenerator()
	case "rust":
		// Use embedded generator if outputting to an embedded module location
		if isEmbeddedRustLocation(outputDir) {
			gen = rust.NewEmbeddedGenerator()
		} else {
			gen = rust.NewGenerator()
		}
	case "python":
		gen = python.NewGenerator()
	case "css":
		gen = css.NewGenerator()
	case "dart":
		fmt.Printf("⚠ %s generator not yet implemented (coming in v1.0.0)\n", lang)
		return nil
	default:
		return errors.Newf("unknown language: %s", lang)
	}

	// Generate types for all packages
	results, typeToPackage, err := generateTypesForPackages(packages, gen)
	if err != nil {
		return errors.Wrap(err, "failed to generate types for packages")
	}

	// Add cross-package imports (TypeScript-specific)
	if lang == "typescript" {
		addCrossPackageImports(results, typeToPackage)
	}

	// outputDir and fileExt already determined above

	// Write generated files
	if err := writeGeneratedOutput(results, outputDir, fileExt, lang); err != nil {
		return errors.Wrap(err, "failed to write generated output")
	}

	// Generate index file for TypeScript (barrel export)
	// Only generate when processing all default packages to avoid partial indices
	if outputDir != "" && lang == "typescript" && generateIndex {
		exports := convertToPackageExports(results)
		if err := typescript.GenerateIndexFile(outputDir, exports); err != nil {
			return errors.Wrap(err, "failed to generate index file")
		}
	}

	// Generate mod.rs index for Rust
	// Only generate when processing all default packages to avoid partial indices
	// Skip scaffolding files (lib.rs, Cargo.toml, README.md, mod.rs) for embedded locations
	if outputDir != "" && lang == "rust" && generateIndex {
		exports := convertToRustPackageExports(results)
		isEmbedded := isEmbeddedRustLocation(outputDir)

		if !isEmbedded {
			// Standalone crate: generate all scaffolding
			if err := rust.GenerateIndexFile(outputDir, exports); err != nil {
				return errors.Wrap(err, "failed to generate mod.rs")
			}
			if err := rust.GenerateLibRs(outputDir, exports); err != nil {
				return errors.Wrap(err, "failed to generate lib.rs")
			}
			if err := rust.GenerateCargoToml(outputDir); err != nil {
				return errors.Wrap(err, "failed to generate Cargo.toml")
			}
			if err := rust.GenerateReadme(outputDir, exports); err != nil {
				return errors.Wrap(err, "failed to generate README.md")
			}
		}
		// Embedded location: skip scaffolding (custom mod.rs exists in parent crate)
	}

	// Generate __init__.py for Python
	// Only generate when processing all default packages to avoid partial indices
	if outputDir != "" && lang == "python" && generateIndex {
		exports := convertToPythonPackageExports(results)
		if err := python.GenerateInitFile(outputDir, exports); err != nil {
			return errors.Wrap(err, "failed to generate __init__.py")
		}
		if err := python.GeneratePyProjectToml(outputDir); err != nil {
			return errors.Wrap(err, "failed to generate pyproject.toml")
		}
		if err := python.GenerateReadme(outputDir, exports); err != nil {
			return errors.Wrap(err, "failed to generate README.md")
		}
	}

	// Generate README.md for CSS
	// Only generate when processing all default packages to avoid partial indices
	if outputDir != "" && lang == "css" && generateIndex {
		if err := css.GenerateReadme(outputDir); err != nil {
			return fmt.Errorf("failed to generate CSS README: %w", err)
		}
		fmt.Printf("✓ Generated %s (index)\n", filepath.Join(outputDir, "README.md"))
	}

	// Generate README.md index for markdown documentation
	// Only generate when processing all default packages to avoid partial indices
	if outputDir != "" && lang == "markdown" && generateIndex {
		readme := generateMarkdownIndex(results)
		readmePath := filepath.Join(outputDir, "README.md")
		if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
			return errors.Wrap(err, "failed to write README")
		}
		fmt.Printf("✓ Generated %s (index)\n", readmePath)

		// Also generate API documentation (part of markdown output)
		apiOutputDir := "docs/api"
		if err := api.GenerateAPIDoc("server", apiOutputDir); err != nil {
			return errors.Wrap(err, "failed to generate API docs")
		}
	}

	return nil
}

// generateTypesForPackages generates types for all packages (first pass)
func generateTypesForPackages(packages []string, gen typegen.Generator) ([]genResult, map[string]string, error) {
	results := make([]genResult, 0, len(packages))
	typeToPackage := make(map[string]string) // typeName -> packageName

	for _, pkg := range packages {
		result, err := typegen.GenerateFromPackage(pkg, gen)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to generate types for %s", pkg)
		}

		// Generate output file
		output := gen.GenerateFile(result)

		typeNames := make([]string, 0, len(result.Types))
		for name := range result.Types {
			typeNames = append(typeNames, name)
			typeToPackage[name] = result.PackageName
		}

		constNames := make([]string, 0, len(result.Consts))
		for name := range result.Consts {
			constNames = append(constNames, name)
		}

		arrayNames := make([]string, 0, len(result.Arrays))
		for name := range result.Arrays {
			arrayNames = append(arrayNames, name)
		}

		mapNames := make([]string, 0, len(result.Maps))
		for name := range result.Maps {
			mapNames = append(mapNames, name)
		}

		results = append(results, genResult{
			packageName: result.PackageName,
			output:      output,
			typeNames:   typeNames,
			constNames:  constNames,
			arrayNames:  arrayNames,
			mapNames:    mapNames,
		})
	}

	return results, typeToPackage, nil
}

// addCrossPackageImports adds import statements for cross-package type references (second pass)
func addCrossPackageImports(results []genResult, typeToPackage map[string]string) {
	for i, res := range results {
		imports := typescript.FindRequiredImports(res.output, res.packageName, typeToPackage)
		if len(imports) > 0 {
			results[i].output = typescript.AddImportsToOutput(res.output, imports)
		}
	}
}

// isEmbeddedRustLocation returns true if the output directory is an embedded location
// within an existing Rust crate (not a standalone generated crate)
func isEmbeddedRustLocation(outputDir string) bool {
	// Embedded locations are within crates/ and contain /src/
	return strings.Contains(outputDir, "crates/") && strings.Contains(outputDir, "/src/")
}

// getOutputConfig determines the output directory and file extension for a language
func getOutputConfig(lang string) (outputDir, fileExt string) {
	if typegenOutput == "" {
		// No output specified: markdown defaults to docs/types, rust to embedded crate, css to web/css/generated, others to stdout
		if lang == "markdown" {
			outputDir = "docs/types"
		} else if lang == "rust" {
			outputDir = "crates/qntx/src/types"
		} else if lang == "css" {
			outputDir = "web/css/generated"
		} else {
			outputDir = "" // stdout mode
		}
	} else {
		// Output specified: use it for all languages
		// For Rust, markdown, and CSS, preserve the actual output structure to ensure correct import generation
		if lang == "rust" {
			outputDir = filepath.Join(typegenOutput, "crates/qntx/src/types")
		} else if lang == "markdown" {
			outputDir = filepath.Join(typegenOutput, "docs/types")
		} else if lang == "css" {
			outputDir = filepath.Join(typegenOutput, "web/css/generated")
		} else {
			outputDir = filepath.Join(typegenOutput, lang)
		}
	}

	switch lang {
	case "typescript":
		fileExt = ".ts"
	case "markdown":
		fileExt = ".md"
	case "python":
		fileExt = ".py"
	case "rust":
		fileExt = ".rs"
	case "css":
		fileExt = ".css"
	case "dart":
		fileExt = ".dart"
	}

	return outputDir, fileExt
}

// writeGeneratedOutput writes generated types to stdout or files
func writeGeneratedOutput(results []genResult, outputDir, fileExt, lang string) error {
	for _, res := range results {
		if outputDir == "" {
			// Write to stdout
			fmt.Printf("// Language: %s\n", lang)
			fmt.Printf("// Package: %s\n", res.packageName)
			fmt.Println(res.output)
		} else {
			// Write to file
			// Handle Python keyword conflict: async -> async_
			filename := res.packageName
			if lang == "python" && filename == "async" {
				filename = "async_"
			}
			filename = filename + fileExt
			outputPath := filepath.Join(outputDir, filename)

			// Create directory if needed
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return errors.Wrap(err, "failed to create output directory")
			}

			if err := os.WriteFile(outputPath, []byte(res.output), 0644); err != nil {
				return errors.Wrapf(err, "failed to write %s", outputPath)
			}

			// Format Rust files after writing
			if lang == "rust" {
				if err := rust.FormatFile(outputPath); err != nil {
					return errors.Wrapf(err, "failed to format rust file %s", outputPath)
				}
			}

			fmt.Printf("✓ Generated %s (%d types)\n", outputPath, len(res.typeNames))
		}
	}
	return nil
}

// convertToPackageExports converts genResults to TypeScript PackageExport format
func convertToPackageExports(results []genResult) []typescript.PackageExport {
	exports := make([]typescript.PackageExport, len(results))
	for i, res := range results {
		exports[i] = typescript.PackageExport{
			PackageName: res.packageName,
			TypeNames:   res.typeNames,
		}
	}
	return exports
}

// convertToRustPackageExports converts genResults to Rust PackageExports
func convertToRustPackageExports(results []genResult) []rust.PackageExport {
	exports := make([]rust.PackageExport, len(results))
	for i, res := range results {
		exports[i] = rust.PackageExport{
			PackageName: res.packageName,
			TypeNames:   res.typeNames,
		}
	}
	return exports
}

// convertToPythonPackageExports converts genResults to Python PackageExports
func convertToPythonPackageExports(results []genResult) []python.PackageExport {
	exports := make([]python.PackageExport, len(results))
	for i, res := range results {
		exports[i] = python.PackageExport{
			PackageName: res.packageName,
			TypeNames:   res.typeNames,
			ConstNames:  res.constNames,
			ArrayNames:  res.arrayNames,
			MapNames:    res.mapNames,
		}
	}
	return exports
}

// generateMarkdownIndex creates a README.md index for the docs/types directory
func generateMarkdownIndex(results []genResult) string {
	var sb strings.Builder

	// Package descriptions
	packageDescriptions := map[string]string{
		"types":    "Core attestation types (As, AsCommand, AxFilter)",
		"async":    "Asynchronous job processing with Pulse",
		"budget":   "Cost tracking and budget management",
		"schedule": "Scheduled execution with cron",
		"server":   "WebSocket message types for real-time updates",
		"sym":      "QNTX symbol constants and collections",
	}

	sb.WriteString("# QNTX Type Definitions\n\n")
	sb.WriteString("Auto-generated documentation showing Go source code alongside TypeScript type definitions.\n\n")
	sb.WriteString("> **Purpose**: Provides a single source of truth for type definitions across different contexts ")
	sb.WriteString("(ChatGPT projects, documentation, etc.) to prevent type drift.\n\n")

	sb.WriteString("## Packages\n\n")

	// Group packages by category
	corePackages := []string{"types", "sym"}
	pulsePackages := []string{"async", "budget", "schedule"}
	serverPackages := []string{"server"}

	writePackageSection := func(title string, packages []string) {
		if len(packages) == 0 {
			return
		}
		sb.WriteString(fmt.Sprintf("### %s\n\n", title))
		for _, pkg := range packages {
			for _, res := range results {
				if res.packageName == pkg {
					desc := packageDescriptions[pkg]
					if desc == "" {
						desc = fmt.Sprintf("%s types", pkg)
					}
					sb.WriteString(fmt.Sprintf("- **[%s](./%s.md)** - %s (%d types)\n",
						pkg, pkg, desc, len(res.typeNames)))
					break
				}
			}
		}
		sb.WriteString("\n")
	}

	writePackageSection("Core Types", corePackages)
	writePackageSection("Pulse System", pulsePackages)
	writePackageSection("Server", serverPackages)

	sb.WriteString("## Usage\n\n")
	sb.WriteString("### For LLM Contexts (ChatGPT, Claude Projects)\n\n")
	sb.WriteString("Copy the relevant markdown file into your project context to ensure type consistency:\n\n")
	sb.WriteString("```\n")
	sb.WriteString("Project Files/\n")
	sb.WriteString("  └── qntx-types.md  (copy from docs/types/)\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### For Development\n\n")
	sb.WriteString("Use as reference when:\n")
	sb.WriteString("- Writing TypeScript code that interfaces with QNTX\n")
	sb.WriteString("- Understanding the shape of API responses\n")
	sb.WriteString("- Debugging type mismatches\n\n")

	sb.WriteString("### For Documentation\n\n")
	sb.WriteString("Link to specific types in your docs:\n")
	sb.WriteString("```markdown\n")
	sb.WriteString("See [Job type](./docs/types/async.md#job) for details.\n")
	sb.WriteString("```\n\n")

	sb.WriteString("## Regeneration\n\n")
	sb.WriteString("These files are automatically regenerated from Go source code:\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("make types              # Regenerate all types\n")
	sb.WriteString("make types-check        # Verify types are up to date (CI)\n")
	sb.WriteString("```\n\n")
	sb.WriteString("**Do not edit manually** - changes will be overwritten.\n\n")

	sb.WriteString("## Format\n\n")
	sb.WriteString("Each type is shown side-by-side:\n\n")
	sb.WriteString("| Go Source | TypeScript |\n")
	sb.WriteString("|-----------|------------|\n")
	sb.WriteString("| Full struct with tags | Generated interface |\n\n")
	sb.WriteString("This makes it easy to:\n")
	sb.WriteString("- See the canonical Go definition\n")
	sb.WriteString("- Understand the TypeScript mapping\n")
	sb.WriteString("- Verify struct tags are correct\n")
	sb.WriteString("- Cross-reference between languages\n\n")

	sb.WriteString("---\n\n")
	sb.WriteString("*Generated by `typegen`*\n")

	return sb.String()
}
