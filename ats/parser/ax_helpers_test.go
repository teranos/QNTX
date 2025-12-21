package parser

import (
	"testing"

	"github.com/teranos/QNTX/ats/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test fixtures using famous linguists as characters
// Chomsky - transformational grammar pioneer
// Saussure - father of modern linguistics
// Jakobson - phonology and structuralism
// Pinker - psycholinguistics
// Labov - sociolinguistics
// Sapir - Sapir-Whorf hypothesis
// Bloomfield - American structuralism

// TestClassifyKeyword verifies the keyword classification system
func TestClassifyKeyword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected keywordType
	}{
		// Grammatical connectors
		{"is as grammatical", "is", keywordGrammatical},
		{"are as grammatical", "are", keywordGrammatical},
		{"IS uppercase grammatical", "IS", keywordGrammatical},
		{"is phrase prefix", "is linguist", keywordGrammatical},
		{"are phrase prefix", "are researchers", keywordGrammatical},

		// Context transitions
		{"of as context transit", "of", keywordContextTransit},
		{"from as context transit", "from", keywordContextTransit},
		{"OF uppercase context transit", "OF", keywordContextTransit},

		// Actor transitions
		{"by as actor transit", "by", keywordActorTransit},
		{"via as actor transit", "via", keywordActorTransit},

		// Temporal keywords
		{"since as temporal", "since", keywordTemporal},
		{"until as temporal", "until", keywordTemporal},
		{"on as temporal", "on", keywordTemporal},
		{"between as temporal", "between", keywordTemporal},
		{"over as temporal", "over", keywordTemporal},

		// So/Action keywords
		{"so as action", "so", keywordSoAction},
		{"therefore as action", "therefore", keywordSoAction},

		// Natural predicates
		{"speaks as natural predicate", "speaks", keywordNaturalPredicate},
		{"knows as natural predicate", "knows", keywordNaturalPredicate},
		{"has_experience as natural predicate", "has_experience", keywordNaturalPredicate},

		// Regular words (not keywords - formerly "profession keywords")
		{"linguist not a keyword", "linguist", keywordNone},
		{"researcher not a keyword", "researcher", keywordNone},
		{"analyst not a keyword", "analyst", keywordNone},
		{"CHOMSKY not a keyword", "CHOMSKY", keywordNone},
		{"linguistics not a keyword", "linguistics", keywordNone},
		{"MIT not a keyword", "MIT", keywordNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyKeyword(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsGrammaticalToken tests the grammatical token detection
func TestIsGrammaticalToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"is alone", "is", true},
		{"are alone", "are", true},
		{"is with space prefix", "is linguist", true},
		{"are with space prefix", "are linguists", true},
		{"IS uppercase", "IS", true},
		{"of not grammatical", "of", false},
		{"by not grammatical", "by", false},
		{"CHOMSKY not grammatical", "CHOMSKY", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGrammaticalToken(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsContextKeyword tests context keyword detection
func TestIsContextKeyword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"of is context", "of", true},
		{"from is context", "from", true},
		{"by is context", "by", true},
		{"via is context", "via", true},
		{"at is context", "at", true},
		{"in is context", "in", true},
		{"for is context", "for", true},
		{"with is context", "with", true},
		{"OF uppercase", "OF", true},
		{"linguistics not context", "linguistics", false},
		{"CHOMSKY not context", "CHOMSKY", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContextKeyword(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestHandleInitialToken tests first token special case handling
func TestHandleInitialToken(t *testing.T) {
	tests := []struct {
		name           string
		tokens         []string
		expectedState  parseState
		expectedPos    int
		checkPredicate string
	}{
		{
			name:          "empty tokens returns subjects state",
			tokens:        []string{},
			expectedState: stateSubjects,
			expectedPos:   0,
		},
		{
			name:          "CHOMSKY as subject",
			tokens:        []string{"CHOMSKY"},
			expectedState: stateSubjects,
			expectedPos:   0,
		},
		{
			name:          "speaks as natural predicate - Labov scenario",
			tokens:        []string{"speaks", "English"},
			expectedState: statePredicates,
			expectedPos:   0,
		},
		{
			name:          "has_experience as natural predicate - Pinker scenario",
			tokens:        []string{"has_experience", "psycholinguistics"},
			expectedState: statePredicates,
			expectedPos:   0,
		},
		{
			name:          "over temporal at start - Bloomfield scenario",
			tokens:        []string{"over", "5y"},
			expectedState: stateSubjects,
			expectedPos:   2,
		},
		{
			name:          "over without value - Sapir scenario",
			tokens:        []string{"over"},
			expectedState: stateSubjects,
			expectedPos:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &axParser{
				tokens:   preprocessAxTokens(tt.tokens),
				position: 0,
				warnings: []string{},
			}
			filter := &types.AxFilter{Limit: 100, Format: "table"}

			state := parser.handleInitialToken(filter)

			assert.Equal(t, tt.expectedState, state, "state mismatch")
			assert.Equal(t, tt.expectedPos, parser.position, "position mismatch")
			if tt.checkPredicate != "" {
				require.Len(t, filter.Predicates, 1)
				assert.Equal(t, tt.checkPredicate, filter.Predicates[0])
			}
		})
	}
}

// TestHandleOverKeyword tests "over" temporal comparison handling
func TestHandleOverKeyword(t *testing.T) {
	tests := []struct {
		name          string
		tokens        []string
		startPos      int
		expectedDelta int
		checkOver     bool
		overValue     float64
		overUnit      string
	}{
		{
			name:          "over 5y - Chomsky's tenure",
			tokens:        []string{"CHOMSKY", "over", "5y"},
			startPos:      1,
			expectedDelta: 2,
			checkOver:     true,
			overValue:     5,
			overUnit:      "y",
		},
		{
			name:          "over 10y - Saussure's legacy",
			tokens:        []string{"SAUSSURE", "over", "10y"},
			startPos:      1,
			expectedDelta: 2,
			checkOver:     true,
			overValue:     10,
			overUnit:      "y",
		},
		{
			name:          "over 6m - Jakobson's project",
			tokens:        []string{"JAKOBSON", "over", "6m"},
			startPos:      1,
			expectedDelta: 2,
			checkOver:     true,
			overValue:     6,
			overUnit:      "m",
		},
		{
			name:          "over without value - incomplete",
			tokens:        []string{"PINKER", "over"},
			startPos:      1,
			expectedDelta: 1,
			checkOver:     false,
		},
		{
			name:          "over with invalid unit - warning case",
			tokens:        []string{"LABOV", "over", "5x"},
			startPos:      1,
			expectedDelta: 2,
			checkOver:     false, // Invalid unit won't set filter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &axParser{
				tokens:   preprocessAxTokens(tt.tokens),
				position: tt.startPos,
				warnings: []string{},
			}
			filter := &types.AxFilter{Limit: 100, Format: "table"}

			delta := parser.handleOverKeyword(filter)

			assert.Equal(t, tt.expectedDelta, delta, "position delta mismatch")
			if tt.checkOver {
				require.NotNil(t, filter.OverComparison)
				assert.Equal(t, tt.overValue, filter.OverComparison.Value)
				assert.Equal(t, tt.overUnit, filter.OverComparison.Unit)
			}
		})
	}
}

// TestHandleKeywordTransition tests state transitions for various keyword types
func TestHandleKeywordTransition(t *testing.T) {
	tests := []struct {
		name          string
		kwType        keywordType
		lowerToken    string
		initialState  parseState
		expectedState parseState
		expectedDelta int
		handled       bool
	}{
		{
			name:          "context transition - of",
			kwType:        keywordContextTransit,
			lowerToken:    "of",
			initialState:  statePredicates,
			expectedState: stateContexts,
			expectedDelta: 1,
			handled:       true,
		},
		{
			name:          "actor transition - by",
			kwType:        keywordActorTransit,
			lowerToken:    "by",
			initialState:  stateContexts,
			expectedState: stateActors,
			expectedDelta: 1,
			handled:       true,
		},
		{
			name:          "temporal transition - since",
			kwType:        keywordTemporal,
			lowerToken:    "since",
			initialState:  stateActors,
			expectedState: stateSubjects,
			handled:       true,
			// delta varies based on temporal parsing
		},
		{
			name:          "so action transition",
			kwType:        keywordSoAction,
			lowerToken:    "so",
			initialState:  stateContexts,
			expectedState: stateSo,
			expectedDelta: 1,
			handled:       true,
		},
		{
			name:          "none keyword - no transition",
			kwType:        keywordNone,
			lowerToken:    "CHOMSKY",
			initialState:  stateSubjects,
			expectedState: stateSubjects,
			expectedDelta: 0,
			handled:       false,
		},
		{
			name:          "natural predicate - no transition in main loop",
			kwType:        keywordNaturalPredicate,
			lowerToken:    "speaks",
			initialState:  stateSubjects,
			expectedState: stateSubjects,
			expectedDelta: 0,
			handled:       false,
		},
		{
			name:          "regular word (formerly profession) - no transition in main loop",
			kwType:        keywordNone,
			lowerToken:    "linguist",
			initialState:  stateSubjects,
			expectedState: stateSubjects,
			expectedDelta: 0,
			handled:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &axParser{
				tokens:   preprocessAxTokens([]string{"CHOMSKY", tt.lowerToken, "MIT"}),
				position: 1,
				warnings: []string{},
			}
			filter := &types.AxFilter{Limit: 100, Format: "table"}
			currentTokens := []string{}

			newState, delta, handled := parser.handleKeywordTransition(
				tt.kwType, tt.lowerToken, tt.initialState, currentTokens, filter)

			assert.Equal(t, tt.handled, handled, "handled mismatch")
			assert.Equal(t, tt.expectedState, newState, "state mismatch")
			if tt.kwType != keywordTemporal && handled {
				assert.Equal(t, tt.expectedDelta, delta, "delta mismatch")
			}
		})
	}
}

// TestExtractPredicateFromNaturalLanguage tests natural language predicate extraction
func TestExtractPredicateFromNaturalLanguage(t *testing.T) {
	tests := []struct {
		name               string
		tokenValue         string
		expectedPredicates []string
		expectedContexts   []string
	}{
		{
			name:               "single word predicate - Chomsky speaks",
			tokenValue:         "speaks",
			expectedPredicates: []string{"speaks"},
			expectedContexts:   nil,
		},
		{
			name:               "multi-word with context - is linguist of MIT",
			tokenValue:         "is linguist of MIT",
			expectedPredicates: []string{"linguist"},
			expectedContexts:   []string{"mit"},
		},
		{
			name:               "simple is predicate - is professor",
			tokenValue:         "is professor",
			expectedPredicates: []string{"professor"},
			expectedContexts:   nil,
		},
		{
			name:               "complex title - is senior researcher",
			tokenValue:         "is senior researcher",
			expectedPredicates: []string{"senior researcher"},
			expectedContexts:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &axParser{warnings: []string{}}
			filter := &types.AxFilter{Limit: 100, Format: "table"}
			token := axToken{
				value:           tt.tokenValue,
				naturalLanguage: true,
			}

			parser.extractPredicateFromNaturalLanguage(token, filter)

			assert.Equal(t, tt.expectedPredicates, filter.Predicates, "predicates mismatch")
			if tt.expectedContexts != nil {
				assert.Equal(t, tt.expectedContexts, filter.Contexts, "contexts mismatch")
			}
		})
	}
}

// TestShouldSplitNaturalLanguage tests natural language splitting logic
func TestShouldSplitNaturalLanguage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Should NOT split - single words
		{"single word CHOMSKY", "CHOMSKY", false},
		{"single word linguistics", "linguistics", false},

		// Should split - natural language keywords
		{"speaks English", "speaks English", true},
		{"knows syntax", "knows syntax", true},
		{"has_experience phonology", "has_experience phonology", true},
		{"work MIT", "work MIT", true},
		{"study Prague", "study Prague", true},

		// Should split - transition keywords
		{"of MIT linguistics", "of MIT linguistics", true},
		{"from Harvard", "from Harvard", true},
		{"by Chomsky", "by Chomsky", true},
		{"via conference", "via conference", true},

		// Should split - temporal keywords
		{"since 2020", "since 2020", true},
		{"until tomorrow", "until tomorrow", true},
		{"between dates", "between dates", true},
		{"over 5y experience", "over 5y experience", true},

		// Should split - action keywords
		{"so summarize", "so summarize", true},
		{"therefore export", "therefore export", true},

		// Should NOT split - is/are with context keywords (keep together)
		{"is professor of linguistics", "is professor of linguistics", false},
		{"are members of department", "are members of department", false},

		// Should split - is/are complex titles (4+ words)
		{"is senior research professor emeritus", "is senior research professor emeritus", true},

		// Has/have keywords
		{"has expertise", "has expertise", true},
		{"have published", "have published", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSplitNaturalLanguage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParsePredicateWithContexts tests predicate and context extraction
func TestParsePredicateWithContexts(t *testing.T) {
	tests := []struct {
		name              string
		phrase            string
		expectedPredicate string
		expectedContexts  []string
	}{
		{
			name:              "is linguist of MIT - Chomsky pattern",
			phrase:            "is linguist of MIT",
			expectedPredicate: "linguist",
			expectedContexts:  []string{"mit"},
		},
		{
			name:              "is professor from Geneva - Saussure pattern",
			phrase:            "is professor from Geneva",
			expectedPredicate: "professor",
			expectedContexts:  []string{"geneva"},
		},
		{
			name:              "are researchers at Prague - Jakobson pattern",
			phrase:            "are researchers at Prague",
			expectedPredicate: "researchers",
			expectedContexts:  []string{"prague"},
		},
		{
			name:              "is cognitive scientist - Pinker pattern (no context)",
			phrase:            "is cognitive scientist",
			expectedPredicate: "cognitive scientist",
			expectedContexts:  nil,
		},
		{
			name:              "multiple contexts - is expert in syntax of English",
			phrase:            "is expert in syntax of English",
			expectedPredicate: "expert",
			expectedContexts:  []string{"syntax", "english"},
		},
		{
			name:              "too short phrase",
			phrase:            "is",
			expectedPredicate: "",
			expectedContexts:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate, contexts := parsePredicateWithContexts(tt.phrase)

			assert.Equal(t, tt.expectedPredicate, predicate, "predicate mismatch")
			assert.Equal(t, tt.expectedContexts, contexts, "contexts mismatch")
		})
	}
}

// TestLinguistScenarios tests complete parsing scenarios using famous linguists
func TestLinguistScenarios(t *testing.T) {
	tests := []struct {
		name     string
		query    []string
		expected types.AxFilter
	}{
		{
			name:  "Chomsky at MIT - full query",
			query: []string{"CHOMSKY", "is", "professor", "of", "linguistics", "by", "MIT", "since", "yesterday"},
			expected: types.AxFilter{
				Subjects:   []string{"CHOMSKY"},
				Predicates: []string{"professor"},
				Contexts:   []string{"linguistics"},
				Actors:     []string{"mit"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Saussure structuralism query",
			query: []string{"SAUSSURE", "is", "founder", "of", "structuralism"},
			expected: types.AxFilter{
				Subjects:   []string{"SAUSSURE"},
				Predicates: []string{"founder"},
				Contexts:   []string{"structuralism"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Jakobson phonology expertise - is required for predicate transition",
			query: []string{"JAKOBSON", "is", "has_experience", "of", "phonology"},
			expected: types.AxFilter{
				Subjects:   []string{"JAKOBSON"},
				Predicates: []string{"has_experience"},
				Contexts:   []string{"phonology"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Pinker as cognitive scientist",
			query: []string{"PINKER", "is", "scientist", "of", "cognition", "by", "Harvard"},
			expected: types.AxFilter{
				Subjects:   []string{"PINKER"},
				Predicates: []string{"scientist"},
				Contexts:   []string{"cognition"},
				Actors:     []string{"harvard"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Labov sociolinguistics research",
			query: []string{"LABOV", "is", "researcher", "of", "sociolinguistics", "by", "UPenn"},
			expected: types.AxFilter{
				Subjects:   []string{"LABOV"},
				Predicates: []string{"researcher"},
				Contexts:   []string{"sociolinguistics"},
				Actors:     []string{"upenn"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Sapir-Whorf hypothesis query",
			query: []string{"SAPIR", "WHORF", "are", "authors", "of", "hypothesis"},
			expected: types.AxFilter{
				Subjects:   []string{"SAPIR", "WHORF"},
				Predicates: []string{"authors"},
				Contexts:   []string{"hypothesis"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Bloomfield American structuralism",
			query: []string{"BLOOMFIELD", "is", "pioneer", "of", "'American structuralism'"},
			expected: types.AxFilter{
				Subjects:   []string{"BLOOMFIELD"},
				Predicates: []string{"pioneer"},
				Contexts:   []string{"american structuralism"},
				Limit:      100,
				Format:     "table",
			},
		},
		{
			name:  "Over temporal - Chomsky tenure",
			query: []string{"CHOMSKY", "is", "professor", "over", "50y"},
			expected: types.AxFilter{
				Subjects:   []string{"CHOMSKY"},
				Predicates: []string{"professor"},
				Limit:      100,
				Format:     "table",
				// OverComparison will be set
			},
		},
		{
			name:  "So action - export linguists",
			query: []string{"is", "linguist", "so", "export", "csv"},
			expected: types.AxFilter{
				Predicates: []string{"linguist"},
				SoActions:  []string{"export", "csv"},
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
			assert.Equal(t, tt.expected.SoActions, result.SoActions, "SoActions mismatch")
			assert.Equal(t, tt.expected.Limit, result.Limit, "Limit mismatch")
			assert.Equal(t, tt.expected.Format, result.Format, "Format mismatch")
		})
	}
}

// TestHandleGrammaticalToken tests "is"/"are" token processing
func TestHandleGrammaticalToken(t *testing.T) {
	tests := []struct {
		name          string
		token         axToken
		expectedState parseState
	}{
		{
			name: "natural language token with space - Chomsky scenario",
			token: axToken{
				value:           "is professor",
				naturalLanguage: true,
			},
			expectedState: stateContexts,
		},
		{
			name: "natural language single word - Saussure scenario",
			token: axToken{
				value:           "is",
				naturalLanguage: true,
			},
			expectedState: stateContexts,
		},
		{
			name: "multi-word non-natural - Jakobson scenario",
			token: axToken{
				value:           "is researcher",
				naturalLanguage: false,
			},
			expectedState: stateContexts,
		},
		{
			name: "single word non-natural - standard is",
			token: axToken{
				value:           "is",
				naturalLanguage: false,
			},
			expectedState: statePredicates,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := &axParser{warnings: []string{}}
			filter := &types.AxFilter{Limit: 100, Format: "table"}

			state, handled := parser.handleGrammaticalToken(tt.token, filter)

			assert.True(t, handled)
			assert.Equal(t, tt.expectedState, state)
		})
	}
}
