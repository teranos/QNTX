package util

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts PascalCase or camelCase to snake_case.
// Handles acronyms properly (e.g., "HTTPSConnection" -> "https_connection")
func ToSnakeCase(s string) string {
	var result strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Check if we need to insert underscore before this character
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Don't insert underscore if previous char was uppercase (acronym)
			// unless next char is lowercase (end of acronym)
			prevUpper := runes[i-1] >= 'A' && runes[i-1] <= 'Z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if !prevUpper || nextLower {
				result.WriteRune('_')
			}
		}

		result.WriteRune(r)
	}

	return strings.ToLower(result.String())
}

// ToPascalCase converts snake_case or kebab-case to PascalCase
func ToPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})

	var result strings.Builder
	for _, part := range parts {
		if len(part) > 0 {
			// Capitalize first letter, keep rest as-is
			runes := []rune(part)
			result.WriteRune(unicode.ToUpper(runes[0]))
			result.WriteString(string(runes[1:]))
		}
	}

	return result.String()
}

// ToCamelCase converts snake_case or kebab-case to camelCase
func ToCamelCase(s string) string {
	pascal := ToPascalCase(s)
	if len(pascal) == 0 {
		return pascal
	}

	// Lowercase first letter
	runes := []rune(pascal)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}
