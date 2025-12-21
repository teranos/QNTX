package ax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFuzzyPredicateMatching(t *testing.T) {
	matcher := NewPredicateMatcher()
	allPredicates := []string{
		"engineer",
		"software engineer",
		"senior software engineer",
		"principal engineer",
		"manager",
		"engineering manager",
		"product manager",
		"developer",
		"senior developer",
		"team lead",
		"consultant",
	}

	tests := []struct {
		query    string
		expected []string
	}{
		{
			query: "engineer",
			expected: []string{
				"engineer",
				"software engineer",
				"senior software engineer",
				"principal engineer",
				"engineering manager", // word boundary match
			},
		},
		{
			query: "software",
			expected: []string{
				"software engineer",
				"senior software engineer",
			},
		},
		{
			query: "manager",
			expected: []string{
				"manager",
				"engineering manager",
				"product manager",
			},
		},
		{
			query: "senior",
			expected: []string{
				"senior software engineer",
				"senior developer",
			},
		},
		{
			query: "lead",
			expected: []string{
				"team lead",
			},
		},
		{
			query:    "nonexistent",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := matcher.FindMatches(tt.query, allPredicates)
			assert.ElementsMatch(t, tt.expected, result,
				"Query '%s' should match expected predicates", tt.query)
		})
	}
}

func TestFuzzyMatchingCaseInsensitive(t *testing.T) {
	matcher := NewPredicateMatcher()
	allPredicates := []string{
		"Engineer",
		"Software Engineer",
		"PRODUCT MANAGER",
	}

	tests := []struct {
		query    string
		expected []string
	}{
		{
			query: "engineer",
			expected: []string{
				"Engineer",
				"Software Engineer",
			},
		},
		{
			query: "ENGINEER",
			expected: []string{
				"Engineer",
				"Software Engineer",
			},
		},
		{
			query: "product",
			expected: []string{
				"PRODUCT MANAGER",
			},
		},
		{
			query: "Product",
			expected: []string{
				"PRODUCT MANAGER",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := matcher.FindMatches(tt.query, allPredicates)
			assert.ElementsMatch(t, tt.expected, result,
				"Query '%s' should match case-insensitively", tt.query)
		})
	}
}

func TestFuzzyMatchingEdgeCases(t *testing.T) {
	matcher := NewPredicateMatcher()

	tests := []struct {
		name       string
		query      string
		predicates []string
		expected   []string
	}{
		{
			name:       "empty query",
			query:      "",
			predicates: []string{"engineer", "manager"},
			expected:   []string{},
		},
		{
			name:       "whitespace query",
			query:      "   ",
			predicates: []string{"engineer", "manager"},
			expected:   []string{},
		},
		{
			name:       "empty predicates",
			query:      "engineer",
			predicates: []string{},
			expected:   []string{},
		},
		{
			name:       "no matches",
			query:      "nonexistent",
			predicates: []string{"engineer", "manager"},
			expected:   []string{},
		},
		{
			name:       "exact match with whitespace",
			query:      "  engineer  ",
			predicates: []string{"engineer", "software engineer"},
			expected:   []string{"engineer", "software engineer"},
		},
		{
			name:       "predicate with whitespace",
			query:      "engineer",
			predicates: []string{"  engineer  ", "software engineer"},
			expected:   []string{"  engineer  ", "software engineer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matcher.FindMatches(tt.query, tt.predicates)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestFuzzyMatchingWordBoundary(t *testing.T) {
	matcher := NewPredicateMatcher()
	allPredicates := []string{
		"manager",              // exact match
		"engineering manager",  // word boundary match
		"managernow",           // substring but not word boundary
		"product manager lead", // word boundary match
	}

	result := matcher.FindMatches("manager", allPredicates)
	expected := []string{
		"manager",
		"engineering manager",
		"managernow", // substring matching should find this
		"product manager lead",
	}

	assert.ElementsMatch(t, expected, result)
}

func TestRemoveDuplicates(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			input:    []string{},
			expected: []string{},
		},
		{
			input:    []string{"a"},
			expected: []string{"a"},
		},
		{
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
	}

	for _, tt := range tests {
		result := removeDuplicates(tt.input)
		assert.ElementsMatch(t, tt.expected, result)
	}
}

func TestFuzzyContextMatching(t *testing.T) {
	matcher := NewFuzzyMatcher()
	allContexts := []string{
		"ACME Inc",
		"CONSOLIDATED Inc",
		"ENTERPRISE Corporation",
		"PLATFORM Inc",
		"CLOUD Services",
		"PRODUCTS Inc",
	}

	tests := []struct {
		query    string
		expected []string
	}{
		{
			query: "acme",
			expected: []string{
				"ACME Inc", // Substring match
			},
		},
		{
			query: "enterprise",
			expected: []string{
				"ENTERPRISE Corporation",
			},
		},
		{
			query: "ent",
			expected: []string{
				"ENTERPRISE Corporation", // Substring match
			},
		},
		{
			query: "platform",
			expected: []string{
				"PLATFORM Inc",
			},
		},
		{
			query: "svc",
			expected: []string{}, // No abbreviation support
		},
		{
			query: "prod",
			expected: []string{
				"PRODUCTS Inc", // Substring match
			},
		},
		{
			query:    "nonexistent",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := matcher.FindContextMatches(tt.query, allContexts)
			assert.ElementsMatch(t, tt.expected, result,
				"Query '%s' should match expected contexts", tt.query)
		})
	}
}

func TestOrganizationSuffixMatching(t *testing.T) {
	matcher := NewFuzzyMatcher()
	allContexts := []string{
		"Acme Corp",
		"Acme Corporation",
		"Acme Inc",
		"Acme Limited",
		"Acme LLC",
	}

	result := matcher.FindContextMatches("acme", allContexts)
	expected := []string{
		"Acme Corp",
		"Acme Corporation",
		"Acme Inc",
		"Acme Limited",
		"Acme LLC",
	}

	assert.ElementsMatch(t, expected, result,
		"Query 'acme' should match all Acme organizations ignoring suffixes")
}
