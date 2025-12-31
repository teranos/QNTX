package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/typegen"
)

// Default packages to generate types from
var defaultPackages = []string{
	"github.com/teranos/QNTX/ats/types",
	"github.com/teranos/QNTX/pulse/async",
	"github.com/teranos/QNTX/pulse/schedule",
	"github.com/teranos/QNTX/server",
}

var (
	typegenOutput   string
	typegenPackages []string
)

// TypegenCmd represents the typegen command
var TypegenCmd = &cobra.Command{
	Use:   "typegen",
	Short: "Generate TypeScript types from Go source",
	Long: `Generate TypeScript type definitions from Go structs.

This command parses Go source code and generates corresponding TypeScript
interfaces and type aliases. It handles:
  - Struct types → TypeScript interfaces
  - Type aliases with consts → TypeScript union types (enums)
  - JSON tags for field naming
  - Pointer types as optional fields
  - time.Time as string
  - map[string]interface{} as Record<string, unknown>

Examples:
  qntx typegen                           # Generate to stdout
  qntx typegen --output web/types/gen/   # Write to directory
  qntx typegen --packages pulse/async    # Specific package only`,
	RunE: runTypegen,
}

func init() {
	TypegenCmd.Flags().StringVarP(&typegenOutput, "output", "o", "", "Output directory (default: stdout)")
	TypegenCmd.Flags().StringSliceVarP(&typegenPackages, "packages", "p", nil, "Packages to process (default: ats/types, pulse/async, server)")
}

func runTypegen(cmd *cobra.Command, args []string) error {
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

	// First pass: generate all packages and collect type names
	type genResult struct {
		packageName string
		output      string
		typeNames   []string
	}
	results := make([]genResult, 0, len(packages))
	typeToPackage := make(map[string]string) // typeName -> packageName

	for _, pkg := range packages {
		result, err := typegen.GenerateFromPackage(pkg)
		if err != nil {
			return fmt.Errorf("failed to generate types for %s: %w", pkg, err)
		}

		output := typegen.GenerateFile(result)
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

	// Write output
	for _, res := range results {
		if typegenOutput == "" {
			// Write to stdout
			fmt.Printf("// Package: %s\n", res.packageName)
			fmt.Println(res.output)
		} else {
			// Write to file
			filename := res.packageName + ".ts"
			outputPath := filepath.Join(typegenOutput, filename)

			// Create directory if needed
			if err := os.MkdirAll(typegenOutput, 0755); err != nil {
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

// findRequiredImports scans TypeScript output for type references from other packages
func findRequiredImports(output, currentPackage string, typeToPackage map[string]string) map[string][]string {
	imports := make(map[string][]string) // packageName -> []typeName

	// Pattern to find type references in the output
	// Matches: `: TypeName`, `: TypeName[]`, `: TypeName | null`, etc.
	typeRefPattern := regexp.MustCompile(`:\s*([A-Z][A-Za-z0-9]*)(?:\[\])?(?:\s*\|\s*null)?`)
	matches := typeRefPattern.FindAllStringSubmatch(output, -1)

	seen := make(map[string]bool)
	for _, match := range matches {
		typeName := match[1]
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

	// Build import statements
	var importLines []string
	for pkg, types := range imports {
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
