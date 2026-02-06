package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseTemporalExpression(t *testing.T) {
	// Mock time for deterministic testing
	mockNow := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC) // Saturday
	originalTimeNow := timeNow
	timeNow = func() time.Time { return mockNow }
	defer func() { timeNow = originalTimeNow }()

	tests := []struct {
		name        string
		expr        string
		expected    *time.Time
		wantErr     bool
		errContains string
	}{
		// Natural language expressions - present
		{
			name:     "now",
			expr:     "now",
			expected: &mockNow,
		},
		{
			name:     "today",
			expr:     "today",
			expected: &mockNow,
		},

		// Natural language expressions - relative days
		{
			name: "yesterday",
			expr: "yesterday",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, 0, -1)
				return &t
			}(),
		},
		{
			name: "tomorrow",
			expr: "tomorrow",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, 0, 1)
				return &t
			}(),
		},

		// Natural language expressions - weeks
		{
			name: "last week",
			expr: "last week",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, 0, -7)
				return &t
			}(),
		},
		{
			name: "next week",
			expr: "next week",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, 0, 7)
				return &t
			}(),
		},

		// Natural language expressions - months
		{
			name: "last month",
			expr: "last month",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, -1, 0)
				return &t
			}(),
		},
		{
			name: "next month",
			expr: "next month",
			expected: func() *time.Time {
				t := mockNow.AddDate(0, 1, 0)
				return &t
			}(),
		},

		// Named day calculations - last weekdays
		{
			name: "last monday",
			expr: "last monday",
			expected: func() *time.Time {
				// mockNow is Saturday (6), so last Monday is 5 days ago
				t := mockNow.AddDate(0, 0, -5)
				return &t
			}(),
		},
		{
			name: "last tuesday",
			expr: "last tuesday",
			expected: func() *time.Time {
				// mockNow is Saturday (6), so last Tuesday is 4 days ago
				t := mockNow.AddDate(0, 0, -4)
				return &t
			}(),
		},
		{
			name: "last friday",
			expr: "last friday",
			expected: func() *time.Time {
				// mockNow is Saturday (6), so last Friday is 1 day ago
				t := mockNow.AddDate(0, 0, -1)
				return &t
			}(),
		},

		// Named day calculations - next weekdays
		{
			name: "next monday",
			expr: "next monday",
			expected: func() *time.Time {
				// mockNow is Saturday (6), so next Monday is 2 days ahead
				t := mockNow.AddDate(0, 0, 2)
				return &t
			}(),
		},
		{
			name: "next friday",
			expr: "next friday",
			expected: func() *time.Time {
				// mockNow is Saturday (6), so next Friday is 6 days ahead
				t := mockNow.AddDate(0, 0, 6)
				return &t
			}(),
		},

		// ISO date formats
		{
			name: "ISO date",
			expr: "2024-01-01",
			expected: func() *time.Time {
				t := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				return &t
			}(),
		},
		{
			name: "ISO datetime with timezone",
			expr: "2024-01-01T14:30:00Z",
			expected: func() *time.Time {
				t := time.Date(2024, 1, 1, 14, 30, 0, 0, time.UTC)
				return &t
			}(),
		},
		{
			name: "ISO datetime with milliseconds",
			expr: "2023-02-14T09:15:30.123Z",
			expected: func() *time.Time {
				t := time.Date(2023, 2, 14, 9, 15, 30, 123000000, time.UTC)
				return &t
			}(),
		},

		// Error cases - invalid formats
		{
			name:        "invalid date format",
			expr:        "not-a-date",
			wantErr:     true,
			errContains: "unable to parse temporal expression",
		},
		{
			name:        "empty string",
			expr:        "",
			wantErr:     true,
			errContains: "empty temporal expression",
		},
		{
			name:        "malformed ISO date",
			expr:        "2024-13-45",
			wantErr:     true,
			errContains: "unable to parse temporal expression",
		},

		// Error cases - logical errors
		{
			name:        "impossible date",
			expr:        "2024-02-30",
			wantErr:     true,
			errContains: "unable to parse temporal expression",
		},

		// Boundary conditions
		{
			name: "far future date",
			expr: "2099-12-31",
			expected: func() *time.Time {
				t := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
				return &t
			}(),
		},
		{
			name: "far past date",
			expr: "1900-01-01",
			expected: func() *time.Time {
				t := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
				return &t
			}(),
		},

		// Leap year handling
		{
			name: "leap year february 29",
			expr: "2024-02-29",
			expected: func() *time.Time {
				t := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
				return &t
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTemporalExpression(tt.expr)

			if tt.wantErr {
				assert.Error(t, err, "Expected error for expression: %s", tt.expr)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains, "Error should contain expected message")
				}
				return
			}

			assert.NoError(t, err, "Unexpected error for expression: %s", tt.expr)
			assert.NotNil(t, result, "Result should not be nil")

			if tt.expected != nil {
				assert.Equal(t, tt.expected.Year(), result.Year(), "Year mismatch for %s", tt.expr)
				assert.Equal(t, tt.expected.Month(), result.Month(), "Month mismatch for %s", tt.expr)
				assert.Equal(t, tt.expected.Day(), result.Day(), "Day mismatch for %s", tt.expr)

				// For exact time expressions, also check hour/minute
				if tt.expr == "now" || tt.expr == "today" {
					assert.Equal(t, tt.expected.Hour(), result.Hour(), "Hour mismatch for %s", tt.expr)
					assert.Equal(t, tt.expected.Minute(), result.Minute(), "Minute mismatch for %s", tt.expr)
				}
			}
		})
	}
}

func TestParseRelativeDuration(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		expected    time.Duration
		wantErr     bool
		errContains string
	}{
		// Valid duration expressions
		{
			name:     "days",
			expr:     "3 days",
			expected: 72 * time.Hour,
		},
		{
			name:     "weeks",
			expr:     "2 weeks",
			expected: 14 * 24 * time.Hour,
		},
		{
			name:     "hours",
			expr:     "5 hours",
			expected: 5 * time.Hour,
		},
		{
			name:     "single day",
			expr:     "1 day",
			expected: 24 * time.Hour,
		},
		{
			name:     "single week",
			expr:     "1 week",
			expected: 7 * 24 * time.Hour,
		},

		// Error cases
		{
			name:        "invalid format",
			expr:        "not a duration",
			wantErr:     true,
			errContains: "invalid duration format",
		},
		{
			name:        "negative duration",
			expr:        "-5 days",
			wantErr:     true,
			errContains: "invalid duration format",
		},
		{
			name:        "empty string",
			expr:        "",
			wantErr:     true,
			errContains: "invalid duration format",
		},
		{
			name:        "unsupported unit",
			expr:        "5 decades",
			wantErr:     true,
			errContains: "unsupported duration unit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseRelativeDuration(tt.expr)

			if tt.wantErr {
				assert.Error(t, err, "Expected error for expression: %s", tt.expr)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains, "Error should contain expected message")
				}
				return
			}

			assert.NoError(t, err, "Unexpected error for expression: %s", tt.expr)
			assert.Equal(t, tt.expected, result, "Duration mismatch for %s", tt.expr)
		})
	}
}

func TestIsTemporalContinuation(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		// Temporal continuation words
		{
			name:     "and continuation",
			value:    "and",
			expected: true,
		},
		{
			name:     "ago continuation",
			value:    "ago",
			expected: true,
		},

		// ISO date components
		{
			name:     "iso date",
			value:    "2024-01-01",
			expected: true,
		},
		{
			name:     "iso datetime",
			value:    "2024-01-01T14:30:00Z",
			expected: true,
		},

		// Time units
		{
			name:     "days unit",
			value:    "days",
			expected: true,
		},
		{
			name:     "weeks unit",
			value:    "weeks",
			expected: true,
		},
		{
			name:     "months unit",
			value:    "months",
			expected: true,
		},
		{
			name:     "hours unit",
			value:    "hours",
			expected: true,
		},

		// Numbers
		{
			name:     "single digit",
			value:    "5",
			expected: true,
		},
		{
			name:     "multi digit",
			value:    "123",
			expected: true,
		},

		// Weekday names
		{
			name:     "monday",
			value:    "monday",
			expected: true,
		},
		{
			name:     "friday",
			value:    "friday",
			expected: true,
		},
		{
			name:     "uppercase weekday",
			value:    "TUESDAY",
			expected: true,
		},

		// Non-temporal words
		{
			name:     "regular word",
			value:    "engineer",
			expected: false,
		},
		{
			name:     "company name",
			value:    "ACME",
			expected: false,
		},
		{
			name:     "empty string",
			value:    "",
			expected: false,
		},
		{
			name:     "special characters",
			value:    "@#$%",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTemporalContinuation(tt.value)
			assert.Equal(t, tt.expected, result, "IsTemporalContinuation mismatch for %s", tt.value)
		})
	}
}


// Benchmark tests for performance validation
func BenchmarkParseTemporalExpression(b *testing.B) {
	expressions := []string{
		"yesterday",
		"last week",
		"2024-01-01",
		"2024-01-01T14:30:00Z",
		"next monday",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		expr := expressions[i%len(expressions)]
		_, _ = ParseTemporalExpression(expr)
	}
}

func BenchmarkIsTemporalContinuation(b *testing.B) {
	values := []string{
		"and", "ago", "2024-01-01", "days", "monday", "engineer", "ACME",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := values[i%len(values)]
		_ = IsTemporalContinuation(value)
	}
}
