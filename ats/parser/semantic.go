package parser

import (
	"strings"

	"github.com/teranos/QNTX/ats/symbols"
)

// SemanticTokenType classifies tokens by grammatical role in ATS queries
type SemanticTokenType string

const (
	SemanticCommand   SemanticTokenType = "command"   // i, am, ix, ax, as
	SemanticKeyword   SemanticTokenType = "keyword"   // is, of, by, since, so
	SemanticSubject   SemanticTokenType = "subject"   // Parsed as subject in query
	SemanticPredicate SemanticTokenType = "predicate" // Parsed as predicate
	SemanticContext   SemanticTokenType = "context"   // Parsed as context
	SemanticActor     SemanticTokenType = "actor"     // Parsed as actor
	SemanticTemporal  SemanticTokenType = "temporal"  // Time expressions
	SemanticSymbol    SemanticTokenType = "symbol"    // ⋈, ∈, ⌬, etc.
	SemanticString    SemanticTokenType = "string"    // Quoted strings
	SemanticURL       SemanticTokenType = "url"       // HTTP(S) URLs
	SemanticUnknown   SemanticTokenType = "unknown"   // Unparsed/unclassified
)

// SemanticToken combines token text with its semantic classification and position
type SemanticToken struct {
	Text         string            `json:"text"`
	Type         SemanticTokenType `json:"semantic_type"`
	Range        Range             `json:"range"`
	Hover        string            `json:"hover,omitempty"`
	IsQuoted     bool              `json:"is_quoted"`
	IsIncomplete bool              `json:"is_incomplete"`
}

// SourceToken extends axToken with position information
type SourceToken struct {
	axToken       // Embed existing token fields
	Range   Range // Source position span
}

// Value returns the token's value
func (st SourceToken) Value() string {
	return st.value
}

// Raw returns the token's raw form (including quotes if present)
func (st SourceToken) Raw() string {
	return st.raw
}

// Quoted returns whether the token was quoted
func (st SourceToken) Quoted() bool {
	return st.quoted
}

// ClassifyToken determines semantic type based on token value and parser state
func ClassifyToken(token axToken, state parseState) SemanticTokenType {
	value := token.value

	// Check for command keywords
	if isCommand(value) {
		return SemanticCommand
	}

	// Check for SEG symbols
	if isSymbol(value) {
		return SemanticSymbol
	}

	// Check for grammatical keywords
	if isKeyword(value) {
		return SemanticKeyword
	}

	// Check for URL
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return SemanticURL
	}

	// Check for quoted strings
	if token.quoted {
		return SemanticString
	}

	// Classify by parser state (grammatical role)
	switch state {
	case stateSubjects:
		return SemanticSubject
	case statePredicates:
		return SemanticPredicate
	case stateContexts:
		return SemanticContext
	case stateActors:
		return SemanticActor
	case stateTemporal:
		return SemanticTemporal
	default:
		return SemanticUnknown
	}
}

// ToSemanticToken converts a SourceToken to SemanticToken with classification
func (st SourceToken) ToSemanticToken(state parseState) SemanticToken {
	return SemanticToken{
		Text:         st.value,
		Type:         ClassifyToken(st.axToken, state),
		Range:        st.Range,
		IsQuoted:     st.quoted,
		IsIncomplete: false,
	}
}

// isCommand checks if value is an ATS command
func isCommand(s string) bool {
	lower := strings.ToLower(s)
	commands := []string{"i", "am", "ix", "ax", "as"}
	for _, cmd := range commands {
		if lower == cmd {
			return true
		}
	}
	return false
}

// isKeyword checks if value is an ATS grammatical keyword
// TODO(issue #3): Refactor to use map for O(1) lookup instead of O(n) linear search
func isKeyword(s string) bool {
	lower := strings.ToLower(s)
	keywords := append(GrammaticalConnectors, ContextTransitionKeywords...)
	keywords = append(keywords, ActorTransitionKeywords...)
	keywords = append(keywords, "since", "until", "on", "between", "over", "so", "therefore", "at")

	for _, kw := range keywords {
		if lower == kw {
			return true
		}
	}
	return false
}

// isSymbol checks if value is a SEG symbol
func isSymbol(s string) bool {
	// Check against canonical symbols
	_, ok := symbols.SymbolToCommand[s]
	return ok
}

// PreprocessAskTokensWithPositions creates SourceTokens from original query string
// This function tokenizes while tracking exact source positions
func PreprocessAskTokensWithPositions(query string) []SourceToken {
	tracker := NewPositionTracker(query)
	tokens := []SourceToken{}

	// Simple tokenization: split by whitespace, preserving quotes
	i := 0
	for i < len(query) {
		// Skip whitespace
		for i < len(query) && (query[i] == ' ' || query[i] == '\t' || query[i] == '\n') {
			tracker.AdvanceBytes(1)
			i++
		}

		if i >= len(query) {
			break
		}

		start := tracker.Mark()

		// Check for quoted string
		if query[i] == '\'' {
			// Find closing quote
			i++
			tracker.AdvanceBytes(1)
			tokenStart := i

			for i < len(query) && query[i] != '\'' {
				i++
				tracker.AdvanceBytes(1)
			}

			if i < len(query) {
				// Include closing quote
				i++
				tracker.AdvanceBytes(1)
			}

			end := tracker.Mark()
			tokenValue := query[tokenStart : i-1] // Exclude quotes

			tokens = append(tokens, SourceToken{
				axToken: axToken{
					value:           tokenValue,
					quoted:          true,
					naturalLanguage: false,
					raw:             query[start.Offset:i],
				},
				Range: RangeFromPositions(start, end),
			})
		} else {
			// Regular token (non-quoted)
			tokenStart := i
			for i < len(query) && query[i] != ' ' && query[i] != '\t' && query[i] != '\n' && query[i] != '\'' {
				i++
				tracker.AdvanceBytes(1)
			}

			end := tracker.Mark()
			tokenValue := query[tokenStart:i]

			tokens = append(tokens, SourceToken{
				axToken: axToken{
					value:           tokenValue,
					quoted:          false,
					naturalLanguage: false,
					raw:             tokenValue,
				},
				Range: RangeFromPositions(start, end),
			})
		}
	}

	return tokens
}
