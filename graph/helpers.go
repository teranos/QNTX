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

// TODO(QNTX #69): Replace literal value heuristics with attestation-based approach
//
// WHY: Current pattern-matching approach violates attestation-first design:
//   - Fragile heuristics (email patterns, phone patterns, "years" detection)
//   - Not transparent or portable (rules hidden in code)
//   - Couples infrastructure to domain assumptions
//
// SOLUTION: Literal values should be attested explicitly:
//
//	as alice age 30 with literal=true
//	as alice email "alice@example.com" with literal=true
//
// This makes literal vs entity distinction:
//   - Explicit and transparent (in attestations, not code)
//   - Domain-agnostic (no hardcoded patterns)
//   - Clipboard-friendly (share literal rules via text)
//
// See: https://github.com/teranos/QNTX/issues/53
//
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
