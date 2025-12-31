package typescript

import (
	"fmt"
	"sort"
	"strings"
)

// FindRequiredImports scans TypeScript output for type references from other packages
// and returns a map of package names to the types they export that are referenced.
func FindRequiredImports(output, currentPackage string, typeToPackage map[string]string) map[string][]string {
	imports := make(map[string][]string) // packageName -> []typeName
	seen := make(map[string]bool)

	// Extract all potential type names from the output
	candidates := extractTypeNames(output)

	for _, typeName := range candidates {
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

// AddImportsToOutput adds import statements after the header comment
func AddImportsToOutput(output string, imports map[string][]string) string {
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

// extractTypeNames finds all PascalCase identifiers in TypeScript output
func extractTypeNames(output string) []string {
	var typeNames []string

	// Strip comments to avoid false positives from JSDoc
	cleanOutput := stripComments(output)

	// Delimiters: space, colon, semicolon, brackets, pipes, angle brackets, parentheses
	delimiters := " :;[]|<>()\n\t,?"

	// Split the output into words
	words := splitByDelimiters(cleanOutput, delimiters)

	for _, word := range words {
		word = strings.TrimSpace(word)

		// Check if it's a PascalCase type name (starts with uppercase letter)
		if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' {
			// Simple check: all alphanumeric (no dots, no special chars)
			if isAlphanumeric(word) {
				typeNames = append(typeNames, word)
			}
		}
	}

	return typeNames
}

// stripComments removes JSDoc comments (/** ... */) and line comments (//) from TypeScript output
func stripComments(output string) string {
	var result strings.Builder
	i := 0

	for i < len(output) {
		// Check for /** ... */ comments
		if i+2 < len(output) && output[i:i+3] == "/**" {
			// Skip until we find */
			end := strings.Index(output[i:], "*/")
			if end != -1 {
				i += end + 2
				continue
			}
		}

		// Check for // comments
		if i+1 < len(output) && output[i:i+2] == "//" {
			// Skip until end of line
			end := strings.IndexByte(output[i:], '\n')
			if end != -1 {
				result.WriteByte('\n') // Keep the newline
				i += end + 1
				continue
			} else {
				// Comment goes to end of file
				break
			}
		}

		result.WriteByte(output[i])
		i++
	}

	return result.String()
}

// isBuiltinType returns true for TypeScript built-in types
func isBuiltinType(name string) bool {
	builtins := map[string]bool{
		"Record": true,
	}
	return builtins[name]
}
