package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/typegen"
	"github.com/teranos/QNTX/ats/typegen/markdown"
	"github.com/teranos/QNTX/ats/typegen/typescript"
)

// Default packages to generate types from
var defaultPackages = []string{
	"github.com/teranos/QNTX/ats/types",
	"github.com/teranos/QNTX/pulse/async",
	"github.com/teranos/QNTX/pulse/budget",
	"github.com/teranos/QNTX/pulse/schedule",
	"github.com/teranos/QNTX/server",
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
Supported languages: TypeScript (more coming in v1.0.0: Python, Rust, Dart)

It handles:
  - Struct types → interfaces/classes
  - Type aliases with consts → union types/enums
  - JSON tags for field naming
  - Pointer types as optional fields
  - time.Time as string
  - map[string]interface{} as Record/dict/HashMap

Examples:
  qntx typegen                                    # Generate TypeScript to stdout
  qntx typegen --lang typescript                  # Explicit language
  qntx typegen --lang all                         # All languages
  qntx typegen --output types/generated/          # Write to directory (creates typescript/ subdir)
  qntx typegen --packages pulse/async             # Specific package only`,
	RunE: runTypegen,
}

func init() {
	TypegenCmd.Flags().StringVarP(&typegenOutput, "output", "o", "", "Output directory (default: stdout)")
	TypegenCmd.Flags().StringSliceVarP(&typegenPackages, "packages", "p", nil, "Packages to process (default: ats/types, pulse/async, server)")
	TypegenCmd.Flags().StringVarP(&typegenLang, "lang", "l", "typescript", "Target language: typescript, python, rust, dart, all")
}

func runTypegen(cmd *cobra.Command, args []string) error {
	// Validate and determine languages to generate
	languages := getLanguages(typegenLang)
	if len(languages) == 0 {
		return fmt.Errorf("invalid language: %s (supported: typescript, python, rust, dart, all)", typegenLang)
	}

	packages := typegenPackages
	if len(packages) == 0 {
		packages = defaultPackages
	}

	// Expand short package names to full import paths
	packages = normalizePackagePaths(packages)

	// Generate for each language
	for _, lang := range languages {
		if err := generateForLanguage(lang, packages); err != nil {
			return err
		}
	}

	return nil
}

// getLanguages returns the list of languages to generate based on the --lang flag
func getLanguages(lang string) []string {
	lang = strings.ToLower(strings.TrimSpace(lang))

	switch lang {
	case "all":
		return []string{"typescript", "markdown"} // TypeScript + Markdown docs
	case "typescript", "ts":
		return []string{"typescript"}
	case "markdown", "md":
		return []string{"markdown"}
	case "python", "py":
		return []string{"python"}
	case "rust", "rs":
		return []string{"rust"}
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
}

// generateForLanguage generates types for a specific language
func generateForLanguage(lang string, packages []string) error {
	// Create the appropriate generator
	var gen typegen.Generator
	switch lang {
	case "typescript":
		gen = typescript.NewGenerator()
	case "markdown":
		gen = markdown.NewGenerator()
	case "python", "rust", "dart":
		fmt.Printf("⚠ %s generator not yet implemented (coming in v1.0.0)\n", lang)
		return nil
	default:
		return fmt.Errorf("unknown language: %s", lang)
	}

	// Generate types for all packages
	results, typeToPackage, err := generateTypesForPackages(packages, gen)
	if err != nil {
		return err
	}

	// Add cross-package imports (TypeScript-specific)
	if lang == "typescript" {
		addCrossPackageImports(results, typeToPackage)
	}

	// Determine output configuration
	outputDir, fileExt := getOutputConfig(lang)

	// Write generated files
	if err := writeGeneratedOutput(results, outputDir, fileExt, lang); err != nil {
		return err
	}

	// Generate index file for TypeScript (barrel export)
	if outputDir != "" && lang == "typescript" {
		exports := convertToPackageExports(results)
		if err := typescript.GenerateIndexFile(outputDir, exports); err != nil {
			return fmt.Errorf("failed to generate index file: %w", err)
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
			return nil, nil, fmt.Errorf("failed to generate types for %s: %w", pkg, err)
		}

		// Generate output file
		output := gen.GenerateFile(result)

		typeNames := make([]string, 0, len(result.Types))
		for name := range result.Types {
			typeNames = append(typeNames, name)
			typeToPackage[name] = result.PackageName
		}

		results = append(results, genResult{
			packageName: result.PackageName,
			output:      output,
			typeNames:   typeNames,
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

// getOutputConfig determines the output directory and file extension for a language
func getOutputConfig(lang string) (outputDir, fileExt string) {
	if typegenOutput == "" {
		// No output specified: markdown defaults to docs/types, others to stdout
		if lang == "markdown" {
			outputDir = "docs/types"
		} else {
			outputDir = "" // stdout mode
		}
	} else {
		// Output specified: use it for all languages
		outputDir = filepath.Join(typegenOutput, lang)
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
			filename := res.packageName + fileExt
			outputPath := filepath.Join(outputDir, filename)

			// Create directory if needed
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			if err := os.WriteFile(outputPath, []byte(res.output), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", outputPath, err)
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
