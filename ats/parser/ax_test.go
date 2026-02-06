//go:build !qntxwasm

package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
)

// axTestFixtures provides realistic query scenarios for testing
func axTestFixtures() []struct {
	name     string
	query    []string
	expected types.AxFilter
	wantErr  bool
} {
	return []struct {
		name     string
		query    []string
		expected types.AxFilter
		wantErr  bool
	}{
		// Core employment & roles
		{
			name:  "core employment query",
			query: []string{"ALICE", "is", "specialist", "of", "acme", "by", "hr", "since", "yesterday"},
			expected: types.AxFilter{
				Subjects:   []string{"ALICE"},
				Predicates: []string{"specialist"},
				Contexts:   []string{"acme"},
				Actors:     []string{"hr"},
				// TimeStart: yesterday
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "quoted context with special characters",
			query: []string{"BOB", "of", "'By Ventures'", "is", "advisor"},
			expected: types.AxFilter{
				Subjects:   []string{"BOB"},
				Predicates: []string{"advisor"},
				Contexts:   []string{"by ventures"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "ISO date temporal",
			query: []string{"CAROL", "is", "specialist", "by", "registry", "since", "2024-01-01"},
			expected: types.AxFilter{
				Subjects:   []string{"CAROL"},
				Predicates: []string{"specialist"},
				Actors:     []string{"registry"},
				// TimeStart: 2024-01-01
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "simple existence query",
			query: []string{"acme", "is", "organization"},
			expected: types.AxFilter{
				Subjects:   []string{"ACME"},
				Predicates: []string{"organization"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "named day temporal",
			query: []string{"ALICE", "is", "specialist", "of", "acme", "since", "last", "monday", "by", "hr"},
			expected: types.AxFilter{
				Subjects:   []string{"ALICE"},
				Predicates: []string{"specialist"},
				Contexts:   []string{"acme"},
				Actors:     []string{"hr"},
				// TimeStart: last monday
				Limit:  100,
				Format: "table",
			},
		},

		// Relationship graphs
		{
			name:  "relationship with underscore predicate",
			query: []string{"FRANK", "is", "introduced", "of", "BOB", "ALICE", "by", "email"},
			expected: types.AxFilter{
				Subjects:   []string{"FRANK"},
				Predicates: []string{"introduced"},
				Contexts:   []string{"bob", "alice"},
				Actors:     []string{"email"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "colleague relationship with date",
			query: []string{"GRACE", "is", "colleague_of", "of", "ALICE", "since", "2023-02-14"},
			expected: types.AxFilter{
				Subjects:   []string{"GRACE"},
				Predicates: []string{"colleague_of"},
				Contexts:   []string{"alice"},
				// TimeStart: 2023-02-14
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "investment relationship",
			query: []string{"HENRY", "is", "invested_in", "of", "BioFrontiers", "by", "angel-network"},
			expected: types.AxFilter{
				Subjects:   []string{"HENRY"},
				Predicates: []string{"invested_in"},
				Contexts:   []string{"biofrontiers"},
				Actors:     []string{"angel-network"},
				Limit:      100,
				Format:     "table",
			},
		},

		// Membership & status
		{
			name:  "membership with quoted org and date",
			query: []string{"INFINITA", "is", "member_of", "of", "'Longevity Consortium'", "since", "2024-06-01"},
			expected: types.AxFilter{
				Subjects:   []string{"INFINITA"},
				Predicates: []string{"member_of"},
				Contexts:   []string{"longevity consortium"},
				// TimeStart: 2024-06-01
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "tagged status",
			query: []string{"JACK", "is", "tagged", "of", "lead", "by", "crm"},
			expected: types.AxFilter{
				Subjects:   []string{"JACK"},
				Predicates: []string{"tagged"},
				Contexts:   []string{"lead"},
				Actors:     []string{"crm"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "status with temporal",
			query: []string{"KAREN", "is", "status", "of", "active", "since", "yesterday"},
			expected: types.AxFilter{
				Subjects:   []string{"KAREN"},
				Predicates: []string{"status"},
				Contexts:   []string{"active"},
				// TimeStart: yesterday
				Limit:  100,
				Format: "table",
			},
		},

		// Events & assertions
		{
			name:  "merger event with specific date",
			query: []string{"LAB-X", "is", "merged_with", "of", "acme", "by", "legal-team", "on", "2024-05-15"},
			expected: types.AxFilter{
				Subjects:   []string{"LAB-X"},
				Predicates: []string{"merged_with"},
				Contexts:   []string{"acme"},
				Actors:     []string{"legal-team"},
				// TimeStart & TimeEnd: 2024-05-15 (same day)
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "verification event",
			query: []string{"MARIA", "is", "email_verified", "by", "onboarding", "since", "2025-01-10"},
			expected: types.AxFilter{
				Subjects:   []string{"MARIA"},
				Predicates: []string{"email_verified"},
				Actors:     []string{"onboarding"},
				// TimeStart: 2025-01-10
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "name change event",
			query: []string{"NETWORKX", "is", "changed_name_to", "of", "netsys", "by", "registrar"},
			expected: types.AxFilter{
				Subjects:   []string{"NETWORKX"},
				Predicates: []string{"changed_name_to"},
				Contexts:   []string{"netsys"},
				Actors:     []string{"registrar"},
				Limit:      100,
				Format:     "table",
			},
		},

		// Grammar flexibility - all synonym combinations
		{
			name:  "are synonym",
			query: []string{"ALICE", "BOB", "are", "specialists"},
			expected: types.AxFilter{
				Subjects:   []string{"ALICE", "BOB"},
				Predicates: []string{"specialists"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "from synonym for context",
			query: []string{"CAROL", "is", "consultant", "from", "external"},
			expected: types.AxFilter{
				Subjects:   []string{"CAROL"},
				Predicates: []string{"consultant"},
				Contexts:   []string{"external"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "via synonym for actor",
			query: []string{"DAVE", "is", "verified", "via", "identity-service"},
			expected: types.AxFilter{
				Subjects:   []string{"DAVE"},
				Predicates: []string{"verified"},
				Actors:     []string{"identity-service"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "all synonyms combined",
			query: []string{"EVE", "are", "contractor", "from", "vendor", "via", "procurement", "since", "last", "week"},
			expected: types.AxFilter{
				Subjects:   []string{"EVE"},
				Predicates: []string{"contractor"},
				Contexts:   []string{"vendor"},
				Actors:     []string{"procurement"},
				// TimeStart: last week
				Limit:  100,
				Format: "table",
			},
		},

		// Multi-actor scenarios
		{
			name:  "multiple actors space-separated",
			query: []string{"FRANK", "is", "analyst", "by", "hr-system", "registry", "github"},
			expected: types.AxFilter{
				Subjects:   []string{"FRANK"},
				Predicates: []string{"analyst"},
				Actors:     []string{"hr-system", "registry", "github"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "multiple actors with temporal",
			query: []string{"GRACE", "is", "manager", "by", "hr", "registry", "since", "2024-01-01"},
			expected: types.AxFilter{
				Subjects:   []string{"GRACE"},
				Predicates: []string{"manager"},
				Actors:     []string{"hr", "registry"},
				// TimeStart: 2024-01-01
				Limit:  100,
				Format: "table",
			},
		},

		// Temporal patterns
		{
			name:  "between temporal range",
			query: []string{"HENRY", "is", "active", "between", "yesterday", "and", "today"},
			expected: types.AxFilter{
				Subjects:   []string{"HENRY"},
				Predicates: []string{"active"},
				// TimeStart: yesterday, TimeEnd: today
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "until temporal",
			query: []string{"IRIS", "is", "intern", "until", "next", "month"},
			expected: types.AxFilter{
				Subjects:   []string{"IRIS"},
				Predicates: []string{"intern"},
				// TimeEnd: next month
				Limit:  100,
				Format: "table",
			},
		},

		// Empty query
		{
			name:  "empty query returns all",
			query: []string{},
			expected: types.AxFilter{
				Limit:  100,
				Format: "table",
			},
		},

		// Simple queries
		{
			name:  "subject only",
			query: []string{"ALICE"},
			expected: types.AxFilter{
				Subjects: []string{"ALICE"},
				Limit:    100,
				Format:   "table",
			},
		},
		{
			name:  "predicate only",
			query: []string{"is", "specialist"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "context only",
			query: []string{"of", "acme"},
			expected: types.AxFilter{
				Contexts: []string{"acme"},
				Limit:    100,
				Format:   "table",
			},
		},
		{
			name:  "actor only",
			query: []string{"by", "hr"},
			expected: types.AxFilter{
				Actors: []string{"hr"},
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "temporal only",
			query: []string{"since", "yesterday"},
			expected: types.AxFilter{
				// TimeStart: yesterday
				Limit:  100,
				Format: "table",
			},
		},
		{
			name:  "bare word treated as subject (not predicate)",
			query: []string{"specialist"},
			expected: types.AxFilter{
				Subjects: []string{"SPECIALIST"}, // Without "is", treated as subject (uppercased)
				Limit:    100,
				Format:   "table",
			},
		},
		{
			name:  "bare words treated as multi-subject query",
			query: []string{"specialist", "google"},
			expected: types.AxFilter{
				Subjects: []string{"SPECIALIST", "GOOGLE"}, // Without keywords, both are subjects
				Limit:    100,
				Format:   "table",
			},
		},
	}
}

func TestParseAxCommand(t *testing.T) {
	tests := axTestFixtures()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Check basic fields
			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
			assert.Equal(t, tt.expected.Actors, result.Actors, "Actors mismatch")
			assert.Equal(t, tt.expected.Limit, result.Limit, "Limit mismatch")
			assert.Equal(t, tt.expected.Format, result.Format, "Format mismatch")

			// Note: Temporal field validation is now handled in temporal_test.go
			// ask_test.go focuses on ask parser integration testing
		})
	}
}

func TestParseAxCommandWarnings(t *testing.T) {
	tests := []struct {
		name             string
		query            []string
		expectedWarnings []string
		shouldParse      bool
	}{
		{
			name:             "empty segment after 'of'",
			query:            []string{"ALICE", "is", "specialist", "of"},
			expectedWarnings: []string{"Failed to process final segment"},
			shouldParse:      true,
		},
		{
			name:             "consecutive anchors 'of by'",
			query:            []string{"ALICE", "is", "specialist", "of", "by", "hr"},
			expectedWarnings: []string{"Failed to process contexts"},
			shouldParse:      true,
		},
		{
			name:             "empty query warning",
			query:            []string{},
			expectedWarnings: []string{"Empty query may return large result set"},
			shouldParse:      true,
		},
		{
			name:             "temporal order warning",
			query:            []string{"since", "tomorrow", "until", "yesterday"},
			expectedWarnings: []string{"Start time is after end time"},
			shouldParse:      true,
		},
		{
			name:             "large limit warning",
			query:            []string{"ALICE", "is", "specialist"},
			expectedWarnings: []string{}, // Would need to set limit > 1000 via flags
			shouldParse:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)

			if tt.shouldParse {
				assert.NotNil(t, result, "Should have parsed successfully")

				// Check if error is a warning
				if err != nil {
					warnings := GetWarnings(err)
					if len(tt.expectedWarnings) > 0 {
						assert.NotEmpty(t, warnings, "Expected warnings but got none")
						for _, expectedWarning := range tt.expectedWarnings {
							found := false
							for _, warning := range warnings {
								if strings.Contains(warning, expectedWarning) {
									found = true
									break
								}
							}
							assert.True(t, found, "Expected warning '%s' not found in %v", expectedWarning, warnings)
						}
					}
				}
			} else {
				assert.Error(t, err, "Should have failed to parse")
			}
		})
	}
}

func TestQuotedKeywordDisambiguation(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
	}{
		{
			name:  "quoted 'by' as literal context",
			query: []string{"MESSAGE", "is", "sent", "of", "'by'"},
			expected: types.AxFilter{
				Subjects:   []string{"MESSAGE"},
				Predicates: []string{"sent"},
				Contexts:   []string{"by"}, // quoted 'by' treated as literal context
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "quoted temporal keyword as context",
			query: []string{"TASK", "is", "scheduled", "of", "'since'"},
			expected: types.AxFilter{
				Subjects:   []string{"TASK"},
				Predicates: []string{"scheduled"},
				Contexts:   []string{"since"}, // quoted 'since' treated as literal context
				Limit:      100,
				Format:     "table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)
			require.NoError(t, err)

			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
			assert.Equal(t, tt.expected.Actors, result.Actors, "Actors mismatch")
		})
	}
}

func TestCaseSensitivityHandling(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
	}{
		{
			name:  "mixed case subjects become uppercase with case-agnostic database resolution",
			query: []string{"alice", "Bob", "CAROL"},
			expected: types.AxFilter{
				Subjects: []string{"ALICE", "BOB", "CAROL"},
				Limit:    100,
				Format:   "table",
			},
		},
		{
			name:  "mixed case contexts become lowercase",
			query: []string{"of", "acme", "Tech_Corp"},
			expected: types.AxFilter{
				Contexts: []string{"acme", "tech_corp"},
				Limit:    100,
				Format:   "table",
			},
		},
		{
			name:  "mixed case actors become lowercase",
			query: []string{"by", "HR", "Registry", "github"},
			expected: types.AxFilter{
				Actors: []string{"hr", "registry", "github"},
				Limit:  100,
				Format: "table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)
			require.NoError(t, err)

			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
			assert.Equal(t, tt.expected.Actors, result.Actors, "Actors mismatch")
		})
	}
}

// TestEnhancedNaturalLanguagePatterns tests enhanced natural language query patterns
func TestEnhancedNaturalLanguagePatterns(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
		wantErr  bool
	}{
		// Natural language patterns - core difference between quoted and separate tokens
		{
			name:  "specialist pattern - extracts meaningful predicate",
			query: []string{"is specialist"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "predicate with context keyword should extract meaningful parts",
			query: []string{"is specialist of RESEARCH_LAB"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"},
				Contexts:   []string{"research_lab"},
				Limit:      100,
				Format:     "table",
			},
		},
		// TODO: Add quoted vs unquoted consistency tests later
		// {
		//   name:  "quoted vs unquoted consistency - quoted form",
		//   query: []string{"'is specialist'"},
		//   expected: types.AxFilter{
		//     Predicates: []string{"specialist"},
		//     Limit:      100,
		//     Format:     "table",
		//   },
		// },
		{
			name:  "specialist pattern - separate tokens become predicate",
			query: []string{"is", "specialist"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "has_experience pattern",
			query: []string{"has_experience"},
			expected: types.AxFilter{
				Predicates: []string{"has_experience"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "experience with context - separate tokens",
			query: []string{"has_experience", "of", "Cloud", "Specialist"},
			expected: types.AxFilter{
				Predicates: []string{"has_experience"},
				Contexts:   []string{"cloud", "specialist"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "experience with quoted context - single token",
			query: []string{"has_experience", "of", "'Cloud Specialist'"},
			expected: types.AxFilter{
				Predicates: []string{"has_experience"},
				Contexts:   []string{"cloud specialist"},
				Limit:      100,
				Format:     "table",
			},
		},

		// Complex profession titles
		// NOTE: Removed test case "is Senior Software Specialist" that expected the parser
		// to have domain knowledge about job titles. Parsers should not know that
		// "Senior Software Specialist" is a job title that belongs in context rather
		// than predicates. This was leaking business logic into the tokenizer.
		{
			name:  "complex profession title - quoted to preserve spacing",
			query: []string{"is", "'Senior Software Specialist'"},
			expected: types.AxFilter{
				Predicates: []string{"Senior Software Specialist"},
				Limit:      100,
				Format:     "table",
			},
		},

		// Multi-word Natural Language Edge Cases
		{
			name:  "quoted multi-word becomes single subject",
			query: []string{"'team lead'"},
			expected: types.AxFilter{
				Subjects: []string{"TEAM LEAD"},
				Limit:    100,
				Format:   "table",
			},
		},
		// TODO: Fix single word parsing without 'is' keyword - see issue #2
		{
			name:  "profession with explicit subject",
			query: []string{"ALICE", "is", "analyst"},
			expected: types.AxFilter{
				Subjects:   []string{"ALICE"},
				Predicates: []string{"analyst"},
				Limit:      100,
				Format:     "table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
			assert.Equal(t, tt.expected.Actors, result.Actors, "Actors mismatch")
			assert.Equal(t, tt.expected.Limit, result.Limit, "Limit mismatch")
			assert.Equal(t, tt.expected.Format, result.Format, "Format mismatch")
		})
	}
}

// TestEnhancedErrorReporting tests the verbosity-aware error reporting with contextual information
func TestEnhancedErrorReporting(t *testing.T) {
	tests := []struct {
		name      string
		query     []string
		verbosity int
		checkErr  func(*testing.T, error)
	}{
		{
			name:      "default verbosity - contextual error without grammar",
			query:     []string{"over", "5q"},
			verbosity: 0,
			checkErr: func(t *testing.T, err error) {
				errStr := err.Error()
				assert.Contains(t, errStr, "Parsing context:")
				assert.Contains(t, errStr, "Position:")
				assert.Contains(t, errStr, "Tokens:")
				assert.Contains(t, errStr, "missing unit in '5q'")
				assert.NotContains(t, errStr, "Grammar:")
			},
		},
		{
			name:      "v2 verbosity - with grammar reference",
			query:     []string{"over", "5q"},
			verbosity: 2,
			checkErr: func(t *testing.T, err error) {
				errStr := err.Error()
				assert.Contains(t, errStr, "Parsing context:")
				assert.Contains(t, errStr, "Position:")
				assert.Contains(t, errStr, "Tokens:")
				assert.Contains(t, errStr, "Grammar:")
				assert.Contains(t, errStr, "[SUBJECTS] [is|are PREDICATES]")
				assert.Contains(t, errStr, "[temporal] [so|therefore ACTIONS]")
			},
		},
		{
			name:      "temporal parsing error with full context",
			query:     []string{"between", "2024", "and", "invalid-date"},
			verbosity: 1,
			checkErr: func(t *testing.T, err error) {
				errStr := err.Error()
				assert.Contains(t, errStr, "invalid start time in 'between'")
				assert.Contains(t, errStr, "Position:")
				assert.Contains(t, errStr, "Token:")       // Structured error uses "Token:" not "Tokens:"
				assert.Contains(t, errStr, "Suggestions:") // Structured error includes suggestions
				assert.Contains(t, errStr, "between")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAxCommandWithVerbosity(tt.query, tt.verbosity)
			require.Error(t, err)
			tt.checkErr(t, err)
		})
	}
}

// TestTokenPositionTracking removed - tested outdated error message formats
// Error handling functionality is still covered by other tests

// TestNaturalLanguageDetection tests the shouldSplitNaturalLanguage logic
func TestNaturalLanguageDetection(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
	}{
		{
			name:  "natural language splitting - is specialist",
			query: []string{"is specialist"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"}, // Extract meaningful predicate
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "natural language splitting - are analysts",
			query: []string{"are analysts"},
			expected: types.AxFilter{
				Predicates: []string{"analysts"}, // Extract meaningful predicate
				Limit:      100,
				Format:     "table",
			},
		},
		// TODO: Fix single word parsing without 'is' keyword - see issue #2
		// TODO: Add back quoted string handling later
		// {
		//   name:  "no splitting for quoted strings",
		//   query: []string{"'is specialist'"},
		//   expected: types.AxFilter{
		//     Subjects: []string{"IS SPECIALIST"},
		//     Limit:    100,
		//     Format:   "table",
		//   },
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)
			require.NoError(t, err)

			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
		})
	}
}

// TestSoActionsHandling tests the 'so' and 'therefore' action parsing
func TestSoActionsHandling(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
	}{
		{
			name:  "so export csv action",
			query: []string{"is", "specialist", "so", "ex", "csv"},
			expected: types.AxFilter{
				Predicates: []string{"specialist"},
				SoActions:  []string{"ex", "csv"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "therefore summarize action",
			query: []string{"has_experience", "therefore", "summarize"},
			expected: types.AxFilter{
				Predicates: []string{"has_experience"},
				SoActions:  []string{"summarize"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "pattern with so summarize",
			query: []string{"has_experience", "of", "'Cloud Specialist'", "therefore", "summarize"},
			expected: types.AxFilter{
				Predicates: []string{"has_experience"},
				Contexts:   []string{"cloud specialist"},
				SoActions:  []string{"summarize"},
				Limit:      100,
				Format:     "table",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAxCommand(tt.query)
			require.NoError(t, err)

			assert.Equal(t, tt.expected.Subjects, result.Subjects, "Subjects mismatch")
			assert.Equal(t, tt.expected.Predicates, result.Predicates, "Predicates mismatch")
			assert.Equal(t, tt.expected.Contexts, result.Contexts, "Contexts mismatch")
			assert.Equal(t, tt.expected.SoActions, result.SoActions, "SoActions mismatch")
		})
	}
}
