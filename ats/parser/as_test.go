package parser

import (
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

func TestParseAsCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected types.AsCommand
		wantErr  bool
	}{
		{
			name: "existence attestation",
			args: []string{"NEO"},
			expected: types.AsCommand{
				Subjects:   []string{"NEO"},
				Predicates: []string{},
				Contexts:   []string{},
				// Actor and Timestamp will be set by defaults
			},
		},
		{
			name: "simple classification",
			args: []string{"NEO", "is", "human"},
			expected: types.AsCommand{
				Subjects:   []string{"NEO"},
				Predicates: []string{"human"},
				Contexts:   []string{},
			},
		},
		{
			name: "batch operation with are",
			args: []string{"NEO", "TYMA", "SHCO", "are", "employees", "of", "ACME"},
			expected: types.AsCommand{
				Subjects:   []string{"NEO", "TYMA", "SHCO"},
				Predicates: []string{"employees"},
				Contexts:   []string{"ACME"},
			},
		},
		{
			name: "quoted predicate",
			args: []string{"TOSH", "is", "'team lead'", "of", "RESEARCH_LAB"},
			expected: types.AsCommand{
				Subjects:   []string{"TOSH"},
				Predicates: []string{"team lead"},
				Contexts:   []string{"RESEARCH_LAB"},
			},
		},
		{
			name: "with explicit actor",
			args: []string{"ALICE", "is", "researcher", "of", "MIT", "by", "registry-system"},
			expected: types.AsCommand{
				Subjects:   []string{"ALICE"},
				Predicates: []string{"researcher"},
				Contexts:   []string{"MIT"},
				Actors:     []string{"registry-system"},
			},
		},
		{
			name: "with multiple actors",
			args: []string{"DAVE", "is", "consultant", "of", "STARTUP", "by", "hr-system", "verification-bot"},
			expected: types.AsCommand{
				Subjects:   []string{"DAVE"},
				Predicates: []string{"consultant"},
				Contexts:   []string{"STARTUP"},
				Actors:     []string{"hr-system", "verification-bot"},
			},
		},
		{
			name: "with explicit date",
			args: []string{"BOB", "is", "analyst", "on", "2025-01-15"},
			expected: types.AsCommand{
				Subjects:   []string{"BOB"},
				Predicates: []string{"analyst"},
				Contexts:   []string{},
				// Timestamp will be parsed to 2025-01-15
			},
		},
		{
			name: "inference rule: JACK manager IBM",
			args: []string{"JACK", "manager", "IBM"},
			expected: types.AsCommand{
				Subjects:   []string{"JACK"},
				Predicates: []string{"manager"},
				Contexts:   []string{"IBM"},
			},
		},
		{
			name: "multiple subjects without keywords (4+ subjects clearly batch)",
			args: []string{"ALICE", "BOB", "CAROL", "DAVE"},
			expected: types.AsCommand{
				Subjects:   []string{"ALICE", "BOB", "CAROL", "DAVE"},
				Predicates: []string{},
				Contexts:   []string{},
			},
		},
		{
			name: "complex with multiple predicates and contexts",
			args: []string{"MORPHEUS", "is", "consultant", "mentor", "of", "ACME", "QNTX", "MIT"},
			expected: types.AsCommand{
				Subjects:   []string{"MORPHEUS"},
				Predicates: []string{"consultant", "mentor"},
				Contexts:   []string{"ACME", "QNTX", "MIT"},
			},
		},
		{
			name: "quoted multi-word predicate",
			args: []string{"JSBE", "is", "'primary care physician'", "of", "'General Hospital'"},
			expected: types.AsCommand{
				Subjects:   []string{"JSBE"},
				Predicates: []string{"primary care physician"},
				Contexts:   []string{"GENERAL HOSPITAL"},
			},
		},
		{
			name: "batch with inference fallback",
			args: []string{"MIKE", "LAIN", "are", "researcher", "analyst", "of", "PROJECT_ALPHA", "PROJECT_BETA"},
			expected: types.AsCommand{
				Subjects:   []string{"MIKE", "LAIN"},
				Predicates: []string{"researcher", "analyst"},
				Contexts:   []string{"PROJECT_ALPHA", "PROJECT_BETA"},
			},
		},
		{
			name: "temporal expression - yesterday",
			args: []string{"EVE", "is", "human", "on", "yesterday"},
			expected: types.AsCommand{
				Subjects:   []string{"EVE"},
				Predicates: []string{"human"},
				Contexts:   []string{},
				// Timestamp will be yesterday
			},
		},
		{
			name: "all components specified",
			args: []string{"NEO", "TYMA", "are", "employees", "contractors", "of", "ACME", "QNTX", "by", "hr-system", "on", "2025-01-15"},
			expected: types.AsCommand{
				Subjects:   []string{"NEO", "TYMA"},
				Predicates: []string{"employees", "contractors"},
				Contexts:   []string{"ACME", "QNTX"},
				Actors:     []string{"hr-system"},
				// Timestamp will be 2025-01-15
			},
		},
		// Error cases
		{
			name:    "empty arguments",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "invalid timestamp",
			args:    []string{"ALICE", "on", "invalid-date"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAsCommand(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAsCommand() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseAsCommand() error = %v", err)
			}

			// Check subjects
			if !equalStringSlices(result.Subjects, tt.expected.Subjects) {
				t.Errorf("ParseAsCommand() subjects = %v, want %v", result.Subjects, tt.expected.Subjects)
			}

			// Check predicates
			if !equalStringSlices(result.Predicates, tt.expected.Predicates) {
				t.Errorf("ParseAsCommand() predicates = %v, want %v", result.Predicates, tt.expected.Predicates)
			}

			// Check contexts
			if !equalStringSlices(result.Contexts, tt.expected.Contexts) {
				t.Errorf("ParseAsCommand() contexts = %v, want %v", result.Contexts, tt.expected.Contexts)
			}

			// Check actors (if specified) - note: LLM actor may be automatically added
			if len(tt.expected.Actors) > 0 {
				// For tests with expected actors, check that they're all present (LLM actor may be added)
				for _, expectedActor := range tt.expected.Actors {
					found := false
					for _, resultActor := range result.Actors {
						if resultActor == expectedActor {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("ParseAsCommand() missing expected actor %s in %v", expectedActor, result.Actors)
					}
				}
			}

			// Check that default actor is set when not specified
			if len(tt.expected.Actors) == 0 && len(result.Actors) == 0 {
				t.Errorf("ParseAsCommand() actors should have default value, got empty")
			}

			// Check timestamp for specific date tests
			if strings.Contains(tt.name, "2025-01-15") {
				expected := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
				if !result.Timestamp.Equal(expected) {
					t.Errorf("ParseAsCommand() timestamp = %v, want %v", result.Timestamp, expected)
				}
			}

			// Check timestamp for yesterday test
			if strings.Contains(tt.name, "yesterday") {
				yesterday := time.Now().AddDate(0, 0, -1)
				if result.Timestamp.Year() != yesterday.Year() ||
					result.Timestamp.Month() != yesterday.Month() ||
					result.Timestamp.Day() != yesterday.Day() {
					t.Errorf("ParseAsCommand() timestamp should be yesterday, got %v", result.Timestamp)
				}
			}
		})
	}
}

func TestTokenizeWithQuotes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "no quotes",
			args:     []string{"ALICE", "is", "specialist"},
			expected: []string{"ALICE", "is", "specialist"},
		},
		{
			name:     "single word in quotes",
			args:     []string{"ALICE", "is", "'specialist'"},
			expected: []string{"ALICE", "is", "specialist"},
		},
		{
			name:     "multi-word in quotes",
			args:     []string{"ALICE", "is", "'primary", "care", "physician'"},
			expected: []string{"ALICE", "is", "primary care physician"},
		},
		{
			name:     "multiple quoted sections",
			args:     []string{"'John", "Doe'", "is", "'senior", "researcher'", "of", "'Research", "Institute'"},
			expected: []string{"John Doe", "is", "senior researcher", "of", "Research Institute"},
		},
		{
			name:     "unclosed quotes",
			args:     []string{"ALICE", "is", "'senior", "researcher"},
			expected: []string{"ALICE", "is", "senior researcher"},
		},
		{
			name:     "empty quotes",
			args:     []string{"ALICE", "is", "''"},
			expected: []string{"ALICE", "is", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenizeWithQuotes(tt.args)
			if !equalStringSlices(result, tt.expected) {
				t.Errorf("tokenizeWithQuotes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseTimeExpression(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
		checkFn func(time.Time) bool
	}{
		{
			name: "now",
			expr: "now",
			checkFn: func(t time.Time) bool {
				return time.Since(t) < time.Minute // Should be very recent
			},
		},
		{
			name: "today",
			expr: "today",
			checkFn: func(t time.Time) bool {
				now := time.Now()
				return t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day()
			},
		},
		{
			name: "yesterday",
			expr: "yesterday",
			checkFn: func(t time.Time) bool {
				yesterday := time.Now().AddDate(0, 0, -1)
				return t.Year() == yesterday.Year() && t.Month() == yesterday.Month() && t.Day() == yesterday.Day()
			},
		},
		{
			name: "ISO date",
			expr: "2025-01-15",
			checkFn: func(t time.Time) bool {
				expected := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
				return t.Equal(expected)
			},
		},
		{
			name: "ISO datetime",
			expr: "2025-01-15T14:30:00Z",
			checkFn: func(t time.Time) bool {
				expected := time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC)
				return t.Equal(expected)
			},
		},
		{
			name:    "invalid format",
			expr:    "not-a-date",
			wantErr: true,
		},
		{
			name:    "empty string",
			expr:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTimeExpression(tt.expr)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimeExpression() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseTimeExpression() error = %v", err)
			}

			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("parseTimeExpression() = %v, failed validation check", result)
			}
		})
	}
}

func TestGetDefaultActor(t *testing.T) {
	// Test the DefaultActorDetector
	detector := &ats.DefaultActorDetector{FallbackActor: "unknown"}
	actor := detector.GetDefaultActor()

	// Should not be empty
	if actor == "" {
		t.Error("DefaultActorDetector.GetDefaultActor() returned empty string")
	}

	// Should contain @ symbol
	if !strings.Contains(actor, "@") {
		t.Errorf("DefaultActorDetector.GetDefaultActor() = %s, should contain '@'", actor)
	}

	// Should be in format ats+user@host
	parts := strings.Split(actor, "@")
	if len(parts) != 2 {
		t.Errorf("DefaultActorDetector.GetDefaultActor() = %s, should be in format 'ats+user@host'", actor)
	}

	userPart := parts[0]
	// Should start with ats+
	if !strings.HasPrefix(userPart, "ats+") {
		t.Errorf("DefaultActorDetector.GetDefaultActor() user part = %s, should start with 'ats+'", userPart)
	}

	// GetLLMActor should return empty string for default detector
	llmActor := detector.GetLLMActor()
	if llmActor != "" {
		t.Errorf("DefaultActorDetector.GetLLMActor() = %s, should return empty string", llmActor)
	}
}

// Helper function to compare string slices
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// Benchmark tests for performance
func BenchmarkParseAsCommand(b *testing.B) {
	args := []string{"NEO", "TYMA", "SHCO", "are", "employees", "of", "ACME", "by", "hr-system"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseAsCommand(args)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTokenizeWithQuotes(b *testing.B) {
	args := []string{"'John", "Doe'", "is", "'primary", "care", "physician'", "of", "'Medical", "Center'"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tokenizeWithQuotes(args)
	}
}
