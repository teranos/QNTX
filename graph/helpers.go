package graph

import (
	"strconv"
	"strings"
)

// normalizeNodeID creates a safe, lowercase node ID for graph visualization.
// It replaces special characters with underscores and converts to lowercase,
// ensuring IDs are valid for use in D3.js and other graph libraries.
// Example: "John@Company" becomes "john_company"
func normalizeNodeID(id string) string {
	// Replace special characters with underscores
	normalized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, id)

	return strings.ToLower(normalized)
}

// isLiteralValue determines if a context should be treated as a literal value
// rather than creating a separate graph node. Literals include numbers, booleans,
// emails, phone numbers, and experience values (e.g., "5 years").
// Non-literals become nodes with relationships in the graph.
func isLiteralValue(context string) bool {
	// Consider numbers, booleans, and certain patterns as literals
	contextLower := strings.ToLower(context)

	// Numeric values - use strconv for idiomatic parsing
	if _, err := strconv.ParseFloat(context, 64); err == nil {
		return true
	}

	// Boolean values - use strconv for idiomatic parsing
	if _, err := strconv.ParseBool(context); err == nil {
		return true
	}

	// Email patterns (likely literals)
	if strings.Contains(context, "@") && strings.Contains(context, ".") {
		return true
	}

	// Phone patterns
	if strings.HasPrefix(context, "+") || strings.HasPrefix(context, "0") {
		if strings.Count(context, "-") >= 1 || strings.Count(context, " ") >= 1 {
			return true
		}
	}

	// Years experience patterns
	if strings.Contains(contextLower, "years") || strings.Contains(contextLower, "y") {
		return true
	}

	// Short values are often literals
	if len(context) <= 3 {
		return true
	}

	return false
}
