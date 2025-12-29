// Package lsp provides Language Server Protocol-inspired language intelligence
// for ATS queries. Unlike standard LSP servers (stdio/JSON-RPC), this implementation
// uses WebSocket transport for browser integration, but maintains LSP concepts
// for semantic tokens, completions, and hover information.
package lsp

import (
	"context"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/storage"
)

// Service provides language intelligence for ATS queries
// Handles parsing with semantic tokens, completions, hover info, and diagnostics
type Service struct {
	index *storage.SymbolIndex
}

// NewService creates a language service instance with the provided symbol index
func NewService(index *storage.SymbolIndex) *Service {
	return &Service{
		index: index,
	}
}

// GetSymbolIndex returns the symbol index for direct access in tests
func (s *Service) GetSymbolIndex() *storage.SymbolIndex {
	return s.index
}

// ParseResponse contains semantic tokens, diagnostics, and parse state
type ParseResponse struct {
	Tokens      []parser.SemanticToken `json:"tokens"`
	Diagnostics []Diagnostic           `json:"diagnostics"`
	ParseState  *ParseState            `json:"parse_state"`
}

// ParseState represents current parser state for autocomplete context
type ParseState struct {
	CurrentState string   `json:"current_state"` // subjects, predicates, contexts, actors, temporal
	Subjects     []string `json:"subjects"`
	Predicates   []string `json:"predicates"`
	Contexts     []string `json:"contexts"`
	Actors       []string `json:"actors"`
	Valid        bool     `json:"valid"`
}

// Diagnostic represents a parse error or warning
type Diagnostic struct {
	Range       parser.Range `json:"range"`
	Severity    string       `json:"severity"` // error, warning, info, hint
	Message     string       `json:"message"`
	Suggestions []string     `json:"suggestions,omitempty"`
}

// Parse analyzes a query and returns semantic tokens with diagnostics
// TODO(issue #111): Consider caching parse results for identical queries
func (s *Service) Parse(ctx context.Context, query string, verbosity int) (*ParseResponse, error) {
	// Tokenize with position tracking
	sourceTokens := parser.PreprocessAskTokensWithPositions(query)

	// Convert to plain askTokens for existing parser
	args := make([]string, len(sourceTokens))
	for i, st := range sourceTokens {
		args[i] = st.Raw()
	}

	// Parse with plain error context (no ANSI codes for web UI)
	filter, parseErr := parser.ParseAxCommandWithContext(args, verbosity, parser.ErrorContextPlain)

	// Build semantic tokens with state classification
	// We'll enhance this to track state during parsing
	semanticTokens := s.classifyTokens(ctx, sourceTokens, filter)

	// Extract diagnostics from parse errors/warnings
	var diagnostics []Diagnostic
	if parseErr != nil {
		// Check if it's a structured ParseError (Phase 3)
		if pe, ok := parseErr.(*parser.ParseError); ok {
			// Use structured error's metadata
			diagData := pe.ToLSPDiagnostic()

			// Extract range
			var diagRange parser.Range
			if rangeData, ok := diagData["range"].(map[string]interface{}); ok {
				startData := rangeData["start"].(map[string]interface{})
				endData := rangeData["end"].(map[string]interface{})
				diagRange = parser.Range{
					Start: parser.Position{
						Line:      startData["line"].(int),
						Character: startData["character"].(int),
						Offset:    startData["offset"].(int),
					},
					End: parser.Position{
						Line:      endData["line"].(int),
						Character: endData["character"].(int),
						Offset:    endData["offset"].(int),
					},
				}
			} else {
				diagRange = inferErrorRange(parseErr, sourceTokens)
			}

			diag := Diagnostic{
				Range:    diagRange,
				Severity: diagData["severity"].(string),
				Message:  diagData["message"].(string),
			}

			// Add suggestions if available
			if suggestions, ok := diagData["suggestions"].([]string); ok && len(suggestions) > 0 {
				diag.Message += "\n\nSuggestions:\n• " + strings.Join(suggestions, "\n• ")
			}

			diagnostics = append(diagnostics, diag)
		} else {
			// Fallback for non-structured errors
			diag := Diagnostic{
				Range:    inferErrorRange(parseErr, sourceTokens),
				Severity: "error",
				Message:  parseErr.Error(),
			}
			diagnostics = append(diagnostics, diag)
		}
	}

	// Build parse state for autocomplete
	state := &ParseState{
		CurrentState: inferCurrentState(sourceTokens, filter),
		Subjects:     filter.Subjects,
		Predicates:   filter.Predicates,
		Contexts:     filter.Contexts,
		Actors:       filter.Actors,
		Valid:        parseErr == nil,
	}

	return &ParseResponse{
		Tokens:      semanticTokens,
		Diagnostics: diagnostics,
		ParseState:  state,
	}, nil
}

// classifyTokens assigns semantic types to tokens based on filter result
func (s *Service) classifyTokens(ctx context.Context, sourceTokens []parser.SourceToken, filter *types.AxFilter) []parser.SemanticToken {
	semanticTokens := make([]parser.SemanticToken, 0, len(sourceTokens))

	// Track state as we iterate through tokens
	currentState := parser.StateSubjects

	for _, srcToken := range sourceTokens {
		value := srcToken.Value()

		// Determine state transitions
		if parser.IsAxKeyword(value) {
			lower := strings.ToLower(value)

			// Transition keywords
			switch lower {
			case "is", "are":
				currentState = parser.StatePredicates
			case "of", "from":
				currentState = parser.StateContexts
			case "by", "via":
				currentState = parser.StateActors
			case "since", "until", "on", "between", "at":
				currentState = parser.StateTemporal
			case "so", "therefore":
				currentState = parser.StateSo
			}
		}

		// Classify token based on current state
		semantic := srcToken.ToSemanticToken(currentState)

		// Add hover information from database
		if semantic.Type == parser.SemanticSubject ||
			semantic.Type == parser.SemanticPredicate ||
			semantic.Type == parser.SemanticContext ||
			semantic.Type == parser.SemanticActor {
			semantic.Hover = s.getHoverInfo(ctx, semantic.Text, semantic.Type)
		}

		semanticTokens = append(semanticTokens, semantic)
	}

	return semanticTokens
}

// getHoverInfo fetches hover information from database
// TODO(issue #143): Enhance with interactive hover - show related attestations
// Current: Simple format "predicate: engineer (5 attestations)"
// Future: Two-column layout with clickable subjects/contexts for exploration
func (s *Service) getHoverInfo(ctx context.Context, text string, tokenType parser.SemanticTokenType) string {
	count := s.index.GetAttestationCount(text, string(tokenType))
	if count == 0 {
		return ""
	}

	return fmt.Sprintf("%s: %s (%d attestations)", tokenType, text, count)
}

// inferErrorRange attempts to determine the range for a parse error
func inferErrorRange(err error, tokens []parser.SourceToken) parser.Range {
	// Default to first token if we can't infer better
	if len(tokens) == 0 {
		return parser.Range{
			Start: parser.Position{Line: 1, Character: 0, Offset: 0},
			End:   parser.Position{Line: 1, Character: 1, Offset: 1},
		}
	}

	// Use last token as error location (often where parsing failed)
	lastToken := tokens[len(tokens)-1]
	return lastToken.Range
}

// inferCurrentState determines what the parser would expect next
func inferCurrentState(tokens []parser.SourceToken, filter *types.AxFilter) string {
	if len(tokens) == 0 {
		return "subjects"
	}

	lastToken := tokens[len(tokens)-1]
	lastValue := strings.ToLower(lastToken.Value())

	// Check last keyword to determine expected next state
	switch lastValue {
	case "is", "are":
		return "predicates"
	case "of", "from":
		return "contexts"
	case "by", "via":
		return "actors"
	case "since", "until", "on", "between", "at":
		return "temporal"
	case "so", "therefore":
		return "actions"
	default:
		// If no keyword, infer from what we've parsed
		if len(filter.Subjects) > 0 && len(filter.Predicates) == 0 {
			return "predicates"
		}
		if len(filter.Predicates) > 0 && len(filter.Contexts) == 0 {
			return "contexts"
		}
		if len(filter.Contexts) > 0 && len(filter.Actors) == 0 {
			return "actors"
		}
		return "subjects"
	}
}

// CompletionRequest represents a completion request
type CompletionRequest struct {
	Query   string
	Line    int
	Cursor  int
	Trigger string // "manual", "auto", "character"
}

// GetCompletions returns context-aware completions
func (s *Service) GetCompletions(ctx context.Context, req CompletionRequest) ([]types.CompletionItem, error) {
	// Parse to determine context
	parseResp, err := s.Parse(ctx, req.Query, 0)
	if err != nil && parseResp == nil {
		return nil, err
	}

	// Extract prefix at cursor position
	prefix := extractPrefix(req.Query, req.Cursor)

	// Get completions based on parse state
	state := parseResp.ParseState.CurrentState

	switch state {
	case "subjects":
		return s.index.GetSubjectCompletions(prefix), nil
	case "predicates":
		return s.index.GetPredicateCompletions(prefix), nil
	case "contexts":
		return s.index.GetContextCompletions(prefix), nil
	case "actors":
		return s.index.GetActorCompletions(prefix), nil
	default:
		// Offer keywords and symbols
		return s.getKeywordCompletions(prefix), nil
	}
}

// getKeywordCompletions returns keyword/symbol completions
func (s *Service) getKeywordCompletions(prefix string) []types.CompletionItem {
	var items []types.CompletionItem

	keywords := []struct {
		label string
		desc  string
	}{
		{"is", "Identity/equivalence"},
		{"are", "Identity/equivalence (plural)"},
		{"of", "Membership/context"},
		{"from", "Origin/context"},
		{"by", "Actor/catalyst"},
		{"via", "Actor/method"},
		{"since", "Temporal start"},
		{"until", "Temporal end"},
		{"on", "Temporal point"},
		{"between", "Temporal range"},
		{"so", "Consequent action"},
	}

	for _, kw := range keywords {
		if strings.HasPrefix(kw.label, strings.ToLower(prefix)) {
			items = append(items, types.CompletionItem{
				Label:         kw.label,
				Kind:          "keyword",
				InsertText:    kw.label,
				Detail:        kw.desc,
				Documentation: fmt.Sprintf("ATS keyword: %s", kw.desc),
				SortText:      "0000",
			})
		}
	}

	return items
}

// extractPrefix gets the word being typed at cursor position
func extractPrefix(query string, cursor int) string {
	if cursor > len(query) {
		cursor = len(query)
	}

	// Find word boundary before cursor
	start := cursor
	for start > 0 && !isWhitespace(query[start-1]) {
		start--
	}

	return query[start:cursor]
}

func isWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n'
}
