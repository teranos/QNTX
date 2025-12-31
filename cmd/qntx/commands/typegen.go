package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/typegen"
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
  qntx typegen --output web/types/generated/      # Write to directory
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
	for i, pkg := range packages {
		if !strings.Contains(pkg, "/") {
			packages[i] = "github.com/teranos/QNTX/" + pkg
		} else if !strings.HasPrefix(pkg, "github.com/") {
			packages[i] = "github.com/teranos/QNTX/" + pkg
		}
	}

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
		return []string{"typescript"} // Only TypeScript for now, will expand in v1.0.0
	case "typescript", "ts":
		return []string{"typescript"}
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

// genResult holds the generated output for a single package
type genResult struct {
	packageName string
	output      string
	typeNames   []string
}

// generateForLanguage generates types for a specific language
func generateForLanguage(lang string, packages []string) error {
	// Only TypeScript is implemented for now
	if lang != "typescript" {
		fmt.Printf("⚠ %s generator not yet implemented (coming in v1.0.0)\n", lang)
		return nil
	}

	// First pass: generate all packages and collect type names
	results := make([]genResult, 0, len(packages))
	typeToPackage := make(map[string]string) // typeName -> packageName

	for _, pkg := range packages {
		result, err := typegen.GenerateFromPackage(pkg)
		if err != nil {
			return fmt.Errorf("failed to generate types for %s: %w", pkg, err)
		}

		// Convert to TypeScript-specific result and generate
		tsResult := &typescript.Result{
			Types:       result.Types,
			PackageName: result.PackageName,
		}
		gen := typescript.NewGenerator()
		output := gen.GenerateFile(tsResult)

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

	// Second pass: add imports for cross-package references
	for i, res := range results {
		imports := findRequiredImports(res.output, res.packageName, typeToPackage)
		if len(imports) > 0 {
			results[i].output = addImportsToOutput(res.output, imports)
		}
	}

	// Determine output directory and file extension
	var outputDir string
	var fileExt string

	if typegenOutput == "" {
		// stdout mode
		outputDir = ""
	} else {
		// File mode - add language subdirectory
		outputDir = filepath.Join(typegenOutput, lang)
	}

	switch lang {
	case "typescript":
		fileExt = ".ts"
	case "python":
		fileExt = ".py"
	case "rust":
		fileExt = ".rs"
	case "dart":
		fileExt = ".dart"
	}

	// Write output
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

	// Generate index file for cleaner imports (only when writing to files)
	if outputDir != "" {
		if err := generateIndexFile(outputDir, results, lang, fileExt); err != nil {
			return fmt.Errorf("failed to generate index file: %w", err)
		}
	}

	return nil
}

// generateIndexFile creates a barrel export file (index.ts) for cleaner imports
func generateIndexFile(outputDir string, results []genResult, lang string, fileExt string) error {
	var sb strings.Builder

	// Header
	sb.WriteString("/* eslint-disable */\n")
	sb.WriteString("// Auto-generated barrel export - re-exports all generated types\n")
	sb.WriteString("// This file is regenerated on every `make types` run\n\n")

	// Sort packages for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].packageName < results[j].packageName
	})

	// Generate exports for each package
	for _, res := range results {
		if len(res.typeNames) == 0 {
			continue
		}

		// Sort type names
		sortedTypes := make([]string, len(res.typeNames))
		copy(sortedTypes, res.typeNames)
		sort.Strings(sortedTypes)

		sb.WriteString(fmt.Sprintf("// Types from %s\n", res.packageName))
		sb.WriteString(fmt.Sprintf("export type {\n"))
		for _, typeName := range sortedTypes {
			sb.WriteString(fmt.Sprintf("  %s,\n", typeName))
		}
		sb.WriteString(fmt.Sprintf("} from './%s';\n\n", res.packageName))
	}

	// Write index file
	indexPath := filepath.Join(outputDir, "index"+fileExt)
	if err := os.WriteFile(indexPath, []byte(sb.String()), 0644); err != nil {
		return err
	}

	fmt.Printf("✓ Generated %s (barrel export)\n", indexPath)
	return nil
}

// findRequiredImports scans TypeScript output for type references from other packages
func findRequiredImports(output, currentPackage string, typeToPackage map[string]string) map[string][]string {
	imports := make(map[string][]string) // packageName -> []typeName
	seen := make(map[string]bool)

	// Parse line by line to find type annotations
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Look for ": TypeName" patterns in TypeScript interface fields
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			continue
		}

		// Extract everything after the colon
		afterColon := strings.TrimSpace(line[colonIdx+1:])

		// Remove array suffix if present: "TypeName[]" -> "TypeName"
		afterColon = strings.TrimSuffix(afterColon, "[]")

		// Remove nullable union if present: "TypeName | null" -> "TypeName"
		if idx := strings.Index(afterColon, " | null"); idx != -1 {
			afterColon = afterColon[:idx]
		}

		// Remove semicolon: "TypeName;" -> "TypeName"
		afterColon = strings.TrimSuffix(strings.TrimSpace(afterColon), ";")

		// Check if it's a PascalCase type name (starts with uppercase)
		if len(afterColon) == 0 || afterColon[0] < 'A' || afterColon[0] > 'Z' {
			continue
		}

		typeName := afterColon

		// Skip if already seen or if it's a built-in type
		if seen[typeName] || isBuiltinType(typeName) {
			continue
		}
		seen[typeName] = true

		// Check if this type is from another package
		if pkg, ok := typeToPackage[typeName]; ok && pkg != currentPackage {
			imports[pkg] = append(imports[pkg], typeName)
		}
	}

	return imports
}

// isBuiltinType returns true for TypeScript built-in types
func isBuiltinType(name string) bool {
	builtins := map[string]bool{
		"Record": true,
	}
	return builtins[name]
}

// addImportsToOutput adds import statements after the header comment
func addImportsToOutput(output string, imports map[string][]string) string {
	if len(imports) == 0 {
		return output
	}

	// Sort package names for deterministic output
	var packageNames []string
	for pkg := range imports {
		packageNames = append(packageNames, pkg)
	}
	sort.Strings(packageNames)

	// Build import statements with sorted packages and types
	var importLines []string
	for _, pkg := range packageNames {
		types := imports[pkg]
		sort.Strings(types) // Sort type names within each import
		importLines = append(importLines, fmt.Sprintf("import { %s } from './%s';", strings.Join(types, ", "), pkg))
	}

	// Find where to insert imports (after the header comments)
	lines := strings.Split(output, "\n")
	var result []string
	headerEnd := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, "//") && line != "" {
			headerEnd = i
			break
		}
	}

	// Insert: header, blank line, imports, blank line, rest
	result = append(result, lines[:headerEnd]...)
	result = append(result, importLines...)
	result = append(result, "")
	result = append(result, lines[headerEnd:]...)

	return strings.Join(result, "\n")
}
