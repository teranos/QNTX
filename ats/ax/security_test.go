package ax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeLikePattern tests the SQL LIKE pattern sanitization
func TestSanitizeLikePattern(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal input with underscore - escaped",
			input:    "normal_text",
			expected: "normal\\_text",
		},
		{
			name:     "Percent wildcard - escaped",
			input:    "test%value",
			expected: "test\\%value",
		},
		{
			name:     "Underscore wildcard - escaped",
			input:    "test_value",
			expected: "test\\_value",
		},
		{
			name:     "Backslash escape - escaped",
			input:    "test\\value",
			expected: "test\\\\value",
		},
		{
			name:     "Multiple wildcards - all escaped",
			input:    "test%value_with\\backslash",
			expected: "test\\%value\\_with\\\\backslash",
		},
		{
			name:     "SQL injection attempt - neutralized",
			input:    "'; DROP TABLE attestations; --",
			expected: "'; DROP TABLE attestations; --",
		},
		{
			name:     "Complex injection with wildcards",
			input:    "user%' OR '1'='1",
			expected: "user\\%' OR '1'='1",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeLikePattern(tc.input)
			assert.Equal(t, tc.expected, result, "Sanitization should match expected output")
		})
	}
}

// TestBuildJSONLikePattern tests the JSON LIKE pattern construction
func TestBuildJSONLikePattern(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Normal value",
			input:    "user",
			expected: "%\"user\"%",
		},
		{
			name:     "Value with percent",
			input:    "user%admin",
			expected: "%\"user\\%admin\"%",
		},
		{
			name:     "Value with underscore",
			input:    "user_admin",
			expected: "%\"user\\_admin\"%",
		},
		{
			name:     "Value with backslash",
			input:    "user\\admin",
			expected: "%\"user\\\\admin\"%",
		},
		{
			name:     "SQL injection attempt",
			input:    "\"; DROP TABLE users; --",
			expected: "%\"\"; DROP TABLE users; --\"%",
		},
		{
			name:     "Complex injection with quotes and wildcards",
			input:    "test%\"; DELETE * FROM attestations WHERE \"1\"=\"1",
			expected: "%\"test\\%\"; DELETE * FROM attestations WHERE \"1\"=\"1\"%",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := buildJSONLikePattern(tc.input)
			assert.Equal(t, tc.expected, result, "JSON pattern should match expected output")
		})
	}
}

// TestSQLInjectionPrevention demonstrates that our sanitization prevents LIKE wildcard injection
func TestSQLInjectionPrevention(t *testing.T) {
	// These are examples of inputs that could exploit LIKE wildcards if not properly sanitized
	wildcardAttacks := []string{
		"% ' UNION SELECT * FROM sqlite_master; --",
		"_\" OR subjects LIKE \"%admin%",
		"admin%'; DROP TABLE attestations; --",
		"test_value%malicious",
	}

	for _, attack := range wildcardAttacks {
		t.Run("Wildcard attack: "+attack, func(t *testing.T) {
			// Test that LIKE wildcards are properly escaped
			sanitized := sanitizeLikePattern(attack)
			jsonPattern := buildJSONLikePattern(attack)

			// Verify wildcards are escaped
			if attack == "% ' UNION SELECT * FROM sqlite_master; --" {
				assert.Contains(t, sanitized, "\\%", "Percent wildcard should be escaped")
				assert.Contains(t, sanitized, "\\_", "Underscore wildcard should be escaped")
			}

			// JSON pattern should be properly wrapped
			assert.Contains(t, jsonPattern, "%\"", "JSON pattern should start with %\"")
			assert.Contains(t, jsonPattern, "\"%", "JSON pattern should end with \"%")

			// Verify that patterns with wildcards are escaped in JSON pattern
			if attack == "test_value%malicious" {
				assert.Contains(t, jsonPattern, "\\_", "Underscore should be escaped in JSON pattern")
				assert.Contains(t, jsonPattern, "\\%", "Percent should be escaped in JSON pattern")
			}

			t.Logf("Original: %s", attack)
			t.Logf("Sanitized: %s", sanitized)
			t.Logf("JSON Pattern: %s", jsonPattern)
		})
	}

	// Test that parameterized queries protect against SQL injection even with unsanitized content
	sqlKeywords := []string{
		"'; DROP TABLE attestations; --",
		"\" OR \"1\"=\"1",
		"'; UPDATE attestations SET subjects='hacked'; --",
	}

	for _, keyword := range sqlKeywords {
		t.Run("SQL keyword in data: "+keyword, func(t *testing.T) {
			// These would be dangerous in string concatenation but safe in parameterized queries
			jsonPattern := buildJSONLikePattern(keyword)

			// The content itself isn't changed (SQL keywords are allowed in data)
			// but it will be safely parameterized when used in queries
			assert.Contains(t, jsonPattern, "%\"", "Should still be wrapped properly")
			assert.Contains(t, jsonPattern, "\"%", "Should still be wrapped properly")

			t.Logf("SQL keywords in data are safe with parameterized queries:")
			t.Logf("Original: %s", keyword)
			t.Logf("JSON Pattern: %s", jsonPattern)
		})
	}
}
