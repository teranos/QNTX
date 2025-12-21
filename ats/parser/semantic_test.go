package parser

import (
	"strings"
	"testing"
)

// TestClassifyToken verifies token classification based on parser state
func TestClassifyToken(t *testing.T) {
	tests := []struct {
		name     string
		tokenVal string
		quoted   bool
		state    parseState
		wantType SemanticTokenType
	}{
		// Commands
		{name: "command i", tokenVal: "i", state: stateSubjects, wantType: SemanticCommand},
		{name: "command ix", tokenVal: "ix", state: stateSubjects, wantType: SemanticCommand},
		{name: "command ax", tokenVal: "ax", state: stateSubjects, wantType: SemanticCommand},
		{name: "command am", tokenVal: "am", state: stateSubjects, wantType: SemanticCommand},
		{name: "command as", tokenVal: "as", state: stateSubjects, wantType: SemanticCommand},

		// Keywords
		{name: "keyword is", tokenVal: "is", state: stateSubjects, wantType: SemanticKeyword},
		{name: "keyword are", tokenVal: "are", state: stateSubjects, wantType: SemanticKeyword},
		{name: "keyword of", tokenVal: "of", state: statePredicates, wantType: SemanticKeyword},
		{name: "keyword by", tokenVal: "by", state: stateContexts, wantType: SemanticKeyword},
		{name: "keyword since", tokenVal: "since", state: stateActors, wantType: SemanticKeyword},

		// Subjects
		{name: "subject", tokenVal: "teacher", state: stateSubjects, wantType: SemanticSubject},
		{name: "quoted subject", tokenVal: "research scientist", quoted: true, state: stateSubjects, wantType: SemanticString},

		// Predicates
		{name: "predicate", tokenVal: "experienced", state: statePredicates, wantType: SemanticPredicate},

		// Contexts
		{name: "context", tokenVal: "company", state: stateContexts, wantType: SemanticContext},

		// Actors
		{name: "actor", tokenVal: "actor1", state: stateActors, wantType: SemanticActor},

		// Temporal
		{name: "temporal", tokenVal: "2024", state: stateTemporal, wantType: SemanticTemporal},

		// URLs
		{name: "url http", tokenVal: "http://example.com", state: stateSubjects, wantType: SemanticURL},
		{name: "url https", tokenVal: "https://example.com", state: stateSubjects, wantType: SemanticURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := axToken{
				value:  tt.tokenVal,
				quoted: tt.quoted,
				raw:    tt.tokenVal,
			}

			got := ClassifyToken(token, tt.state)
			if got != tt.wantType {
				t.Errorf("ClassifyToken(%q, %v) = %v, want %v", tt.tokenVal, tt.state, got, tt.wantType)
			}
		})
	}
}

// TestPreprocessAskTokensWithPositions verifies tokenization with position tracking
func TestPreprocessAskTokensWithPositions(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []struct {
			value     string
			quoted    bool
			startLine int
			startChar int
			endChar   int
		}
	}{
		{
			name:  "simple tokens",
			query: "teacher is experienced",
			want: []struct {
				value     string
				quoted    bool
				startLine int
				startChar int
				endChar   int
			}{
				{"teacher", false, 1, 0, 8},
				{"is", false, 1, 9, 11},
				{"experienced", false, 1, 12, 23},
			},
		},
		{
			name:  "quoted string",
			query: "'research scientist' is skilled",
			want: []struct {
				value     string
				quoted    bool
				startLine int
				startChar int
				endChar   int
			}{
				{"research scientist", true, 1, 0, 19},
				{"is", false, 1, 20, 22},
				{"skilled", false, 1, 23, 30},
			},
		},
		{
			name:  "multi-line query",
			query: "teacher\nis\nexperienced",
			want: []struct {
				value     string
				quoted    bool
				startLine int
				startChar int
				endChar   int
			}{
				{"teacher", false, 1, 0, 8},
				{"is", false, 2, 0, 2},
				{"experienced", false, 3, 0, 11},
			},
		},
		{
			name:  "empty query",
			query: "",
			want: []struct {
				value     string
				quoted    bool
				startLine int
				startChar int
				endChar   int
			}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := PreprocessAskTokensWithPositions(tt.query)

			if len(tokens) != len(tt.want) {
				t.Fatalf("got %d tokens, want %d", len(tokens), len(tt.want))
			}

			for i, want := range tt.want {
				got := tokens[i]

				if got.Value() != want.value {
					t.Errorf("token[%d].Value = %q, want %q", i, got.Value(), want.value)
				}
				if got.Quoted() != want.quoted {
					t.Errorf("token[%d].Quoted = %v, want %v", i, got.Quoted(), want.quoted)
				}
				if got.Range.Start.Line != want.startLine {
					t.Errorf("token[%d].Range.Start.Line = %d, want %d", i, got.Range.Start.Line, want.startLine)
				}
				if got.Range.Start.Character != want.startChar {
					t.Errorf("token[%d].Range.Start.Character = %d, want %d", i, got.Range.Start.Character, want.startChar)
				}
				if got.Range.End.Character != want.endChar {
					t.Errorf("token[%d].Range.End.Character = %d, want %d", i, got.Range.End.Character, want.endChar)
				}
			}
		})
	}
}

// TestToSemanticToken verifies conversion from SourceToken to SemanticToken
func TestToSemanticToken(t *testing.T) {
	source := "teacher is experienced"
	tokens := PreprocessAskTokensWithPositions(source)

	// Test that semantic token conversion preserves information
	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}

	// First token should be classified as subject
	semantic := tokens[0].ToSemanticToken(stateSubjects)
	if semantic.Text != "teacher" {
		t.Errorf("semantic.Text = %q, want %q", semantic.Text, "teacher")
	}
	if semantic.Type != SemanticSubject {
		t.Errorf("semantic.Type = %v, want %v", semantic.Type, SemanticSubject)
	}
	if semantic.IsQuoted {
		t.Error("semantic.IsQuoted = true, want false")
	}

	// Second token should be classified as keyword
	semantic = tokens[1].ToSemanticToken(stateSubjects)
	if semantic.Type != SemanticKeyword {
		t.Errorf("semantic.Type = %v, want %v", semantic.Type, SemanticKeyword)
	}

	// Third token should be classified as predicate
	semantic = tokens[2].ToSemanticToken(statePredicates)
	if semantic.Type != SemanticPredicate {
		t.Errorf("semantic.Type = %v, want %v", semantic.Type, SemanticPredicate)
	}
}

// TestIsAxKeyword verifies keyword detection
func TestIsAxKeyword(t *testing.T) {
	keywords := []string{"is", "are", "of", "from", "by", "via", "since", "until", "on", "between", "so", "therefore"}

	for _, kw := range keywords {
		if !IsAxKeyword(kw) {
			t.Errorf("IsAxKeyword(%q) = false, want true", kw)
		}
		// Test case-insensitivity
		upper := strings.ToUpper(kw)
		if !IsAxKeyword(upper) {
			t.Errorf("IsAxKeyword(%q) = false, want true (case-insensitive)", upper)
		}
	}

	// Test non-keywords
	nonKeywords := []string{"teacher", "analyst", "company", "123"}
	for _, nk := range nonKeywords {
		if IsAxKeyword(nk) {
			t.Errorf("IsAxKeyword(%q) = true, want false", nk)
		}
	}
}
