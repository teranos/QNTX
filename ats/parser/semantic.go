package parser

import (
	"strings"

	"github.com/teranos/QNTX/sym"
)

// keywordLookupMap provides O(1) keyword detection for isKeyword()
var keywordLookupMap map[string]bool

func init() {
	// Build comprehensive keyword map from existing slices + additional temporal/action keywords
	allKeywords := make([]string, 0, 20)
	allKeywords = append(allKeywords, GrammaticalConnectors...)
	allKeywords = append(allKeywords, ContextTransitionKeywords...)
	allKeywords = append(allKeywords, ActorTransitionKeywords...)
	allKeywords = append(allKeywords, "since", "until", "on", "between", "over", "so", "therefore", "at")

	keywordLookupMap = make(map[string]bool, len(allKeywords))
	for _, kw := range allKeywords {
		keywordLookupMap[kw] = true
	}
}

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

// commandLookupMap provides O(1) command detection derived from sym.Commands
var commandLookupMap = func() map[string]bool {
	m := make(map[string]bool, len(sym.Commands))
	for _, cmd := range sym.Commands {
		m[cmd] = true
	}
	return m
}()

// isCommand checks if value is an ATS command (query-initiating token)
func isCommand(s string) bool {
	return commandLookupMap[strings.ToLower(s)]
}

// isKeyword checks if value is an ATS grammatical keyword using O(1) map lookup
func isKeyword(s string) bool {
	return keywordLookupMap[strings.ToLower(s)]
}

// isSymbol checks if value is a SEG symbol
func isSymbol(s string) bool {
	// Check against canonical symbols
	_, ok := sym.SymbolToCommand[s]
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
