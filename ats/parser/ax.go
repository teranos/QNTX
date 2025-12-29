package parser

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/teranos/QNTX/ats/types"
)

// ErrorContext indicates the environment where parser errors will be displayed
type ErrorContext string

const (
	// ErrorContextTerminal indicates errors will be displayed in terminal with ANSI colors
	ErrorContextTerminal ErrorContext = "terminal"
	// ErrorContextPlain indicates errors will be displayed without ANSI codes (web UI, logs, etc)
	ErrorContextPlain ErrorContext = "plain"
)

// Parser keyword constants to avoid duplication and ensure consistency
var (
	// GrammaticalConnectors are words that should be stripped during semantic parsing
	GrammaticalConnectors = []string{"is", "are"}

	// ContextKeywords indicate transitions to context/object information
	ContextKeywords = []string{"of", "from", "by", "via", "at", "in", "for", "with"}

	// ContextTransitionKeywords specifically transition from predicates to contexts
	ContextTransitionKeywords = []string{"of", "from"}

	// ActorTransitionKeywords specifically transition to actor information
	ActorTransitionKeywords = []string{"by", "via"}

	// NaturalPredicates support both singular and plural forms of natural language predicates
	NaturalPredicates = []string{"speak", "speaks", "know", "knows", "work", "worked", "study", "studied", "has_experience", "occupation"}
)

// keywordType represents the classification of a keyword for state transitions
type keywordType int

const (
	keywordNone             keywordType = iota // Not a keyword
	keywordGrammatical                         // "is", "are" - grammatical connectors
	keywordContextTransit                      // "of", "from" - transition to contexts
	keywordActorTransit                        // "by", "via" - transition to actors
	keywordTemporal                            // "since", "until", "on", "between", "over"
	keywordSoAction                            // "so", "therefore" - action keywords
	keywordNaturalPredicate                    // Natural language predicates
)

// Keyword lookup maps for O(1) classification (initialized once)
var (
	grammaticalMap      map[string]bool
	contextTransitMap   map[string]bool
	actorTransitMap     map[string]bool
	temporalMap         map[string]bool
	soActionMap         map[string]bool
	naturalPredicateMap map[string]bool
	contextKeywordMap   map[string]bool
)

// toKeywordMap converts a slice of keywords to a map for O(1) lookup
func toKeywordMap(keywords []string) map[string]bool {
	m := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		m[k] = true
	}
	return m
}

func init() {
	// Initialize all keyword maps for O(1) lookup
	grammaticalMap = toKeywordMap(GrammaticalConnectors)
	contextTransitMap = toKeywordMap(ContextTransitionKeywords)
	actorTransitMap = toKeywordMap(ActorTransitionKeywords)
	temporalMap = toKeywordMap([]string{"since", "until", "on", "between", "over"})
	soActionMap = toKeywordMap([]string{"so", "therefore"})
	naturalPredicateMap = toKeywordMap(NaturalPredicates)
	contextKeywordMap = toKeywordMap(ContextKeywords)
}

// classifyKeyword returns the keyword type for a given token value (case-insensitive)
// Returns keywordNone if the token is not a recognized keyword
func classifyKeyword(value string) keywordType {
	lower := strings.ToLower(value)

	// Check in order of parse priority
	if grammaticalMap[lower] || strings.HasPrefix(lower, "is ") || strings.HasPrefix(lower, "are ") {
		return keywordGrammatical
	}
	if contextTransitMap[lower] {
		return keywordContextTransit
	}
	if actorTransitMap[lower] {
		return keywordActorTransit
	}
	if temporalMap[lower] {
		return keywordTemporal
	}
	if soActionMap[lower] {
		return keywordSoAction
	}
	if naturalPredicateMap[lower] {
		return keywordNaturalPredicate
	}
	return keywordNone
}

// isContextKeyword checks if a word is a context keyword (for natural language splitting)
func isContextKeyword(word string) bool {
	return contextKeywordMap[strings.ToLower(word)]
}

// ParseAxCommand parses natural language ax queries with flexible grammar
// Grammar: qntx ax [SUBJECTS] [is|are PREDICATES] [of|from CONTEXTS] [by|via ACTORS] [temporal] [flags]
func ParseAxCommand(args []string) (*types.AxFilter, error) {
	return ParseAxCommandWithVerbosity(args, 0)
}

// ParseAxCommandWithVerbosity parses ax queries with verbosity for contextual errors
// Defaults to terminal error context (ANSI colors) for CLI usage
func ParseAxCommandWithVerbosity(args []string, verbosity int) (*types.AxFilter, error) {
	return ParseAxCommandWithContext(args, verbosity, ErrorContextTerminal)
}

// ParseAxCommandWithContext parses ax queries with context-aware error formatting
// Use ErrorContextTerminal for terminal output with ANSI colors, ErrorContextPlain for web UI/logs
func ParseAxCommandWithContext(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return parseAxQuery(args, verbosity, ctx)
}

// ParseAskCommand parses natural language ask queries with flexible grammar
// DEPRECATED: Use ParseAxCommand instead. Maintained for backward compatibility only.
// Grammar: qntx ask [SUBJECTS] [is|are PREDICATES] [of|from CONTEXTS] [by|via ACTORS] [temporal] [flags]
func ParseAskCommand(args []string) (*types.AxFilter, error) {
	return ParseAskCommandWithVerbosity(args, 0)
}

// ParseAskCommandWithVerbosity delegates to ParseAxCommandWithVerbosity
func ParseAskCommandWithVerbosity(args []string, verbosity int) (*types.AxFilter, error) {
	return ParseAxCommandWithVerbosity(args, verbosity)
}

// ParseAskCommandWithContext delegates to ParseAxCommandWithContext
func ParseAskCommandWithContext(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return ParseAxCommandWithContext(args, verbosity, ctx)
}

// parseAxQuery contains the actual implementation for ax query parsing
func parseAxQuery(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	filter := &types.AxFilter{
		Limit:  100,
		Format: "table",
	}

	if len(args) == 0 {
		return filter, nil // Empty query returns all
	}

	parser := &axParser{
		tokens:       preprocessAxTokens(args),
		position:     0,
		warnings:     []string{},
		verbosity:    verbosity,
		errorContext: ctx,
	}

	return parser.parse(filter)
}

// axParser handles hybrid state management: state machine for segments + functional for expressions
type axParser struct {
	tokens       []axToken
	position     int
	warnings     []string
	verbosity    int
	errorContext ErrorContext
}

// contextualError creates an error with parsing context information using colors
func (p *axParser) contextualError(message string, args ...interface{}) error {
	return p.contextualErrorWithVerbosity(p.verbosity, message, args...)
}

// structuredError creates a ParseError with full metadata (Phase 3: structured errors)
// Use this for errors that need categorization, severity levels, or suggestions
func (p *axParser) structuredError(kind ErrorKind, message string, args ...interface{}) *ParseError {
	baseMsg := fmt.Sprintf(message, args...)

	err := NewParseError(kind, baseMsg).
		WithPosition(p.position, len(p.tokens))

	// Add current token if available
	if p.position < len(p.tokens) {
		token := p.tokens[p.position]
		err = err.WithToken(&token)
	}

	// Add warnings as context
	if len(p.warnings) > 0 {
		err = err.WithContext("warnings", p.warnings)
	}

	return err
}

// contextualErrorWithVerbosity creates an error with parsing context and optional grammar based on verbosity
func (p *axParser) contextualErrorWithVerbosity(verbosity int, message string, args ...interface{}) error {
	baseMsg := fmt.Sprintf(message, args...)

	// Plain context: concise message without ANSI codes (for web UI, logs, etc)
	if p.errorContext == ErrorContextPlain {
		if p.position >= 0 && len(p.tokens) > 0 {
			return fmt.Errorf("%s (at position %d/%d)", baseMsg, p.position, len(p.tokens))
		}
		return fmt.Errorf("%s", baseMsg)
	}

	// Terminal context: rich formatting with ANSI colors for CLI
	// Build context information with pterm colors
	context := fmt.Sprintf("\n\n%s", pterm.LightCyan("Parsing context:"))
	context += fmt.Sprintf("\n  %s %d/%d", pterm.Yellow("Position:"), p.position, len(p.tokens))

	if len(p.tokens) > 0 {
		context += fmt.Sprintf("\n  %s ", pterm.Yellow("Tokens:"))
		for i, token := range p.tokens {
			if i > 0 {
				context += " "
			}

			var tokenDisplay string
			if i == p.position {
				// Current position - red arrow and bright white token
				tokenDisplay = fmt.Sprintf("%s%s",
					pterm.Red("→"),
					pterm.NewStyle(pterm.FgWhite, pterm.BgRed).Sprintf("[%d]'%s'", i, token.value))
			} else if i == p.position-1 {
				// Last processed - green checkmark and green token
				tokenDisplay = fmt.Sprintf("%s%s",
					pterm.Green("✓"),
					pterm.Green(fmt.Sprintf("[%d]'%s'", i, token.value)))
			} else {
				// Regular token - dim gray
				tokenDisplay = pterm.Gray(fmt.Sprintf("[%d]'%s'", i, token.value))
			}

			if token.quoted {
				tokenDisplay += pterm.Magenta("*") // Purple asterisk for quoted
			}

			context += tokenDisplay
		}
	}

	if len(p.warnings) > 0 {
		context += fmt.Sprintf("\n  %s %s", pterm.Yellow("Warnings:"), pterm.LightYellow(fmt.Sprintf("%v", p.warnings)))
	}

	// Add grammar reference for -vv and higher verbosity
	if verbosity >= 2 {
		context += fmt.Sprintf("\n\n%s %s",
			pterm.Blue("Grammar:"),
			pterm.Cyan("[SUBJECTS] [is|are PREDICATES] [of|from CONTEXTS] [by|via ACTORS] [temporal] [so|therefore ACTIONS]"))
	}

	return fmt.Errorf("%s%s", pterm.Red(baseMsg), context)
}

// stateString returns a human-readable name for the current parsing state
func (state parseState) String() string {
	switch state {
	case stateSubjects:
		return "subjects"
	case statePredicates:
		return "predicates"
	case stateContexts:
		return "contexts"
	case stateActors:
		return "actors"
	case stateTemporal:
		return "temporal"
	case stateSo:
		return "so_actions"
	default:
		return "unknown"
	}
}

type axToken struct {
	value           string
	quoted          bool
	naturalLanguage bool   // true if token came from natural language splitting
	raw             string // original form including quotes
}

type parseState int

const (
	stateSubjects parseState = iota
	statePredicates
	stateContexts
	stateActors
	stateTemporal
	stateSo
)

// Exported constants for external use (e.g., language service)
const (
	StateSubjects   = stateSubjects
	StatePredicates = statePredicates
	StateContexts   = stateContexts
	StateActors     = stateActors
	StateTemporal   = stateTemporal
	StateSo         = stateSo
)

// preprocessAxTokens handles single quote processing and tokenization
func preprocessAxTokens(args []string) []axToken {
	var tokens []axToken

	for _, arg := range args {
		if strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'") && len(arg) > 1 {
			// Single quoted string - treat as literal (ambiguity resolution)
			tokens = append(tokens, axToken{
				value:           arg[1 : len(arg)-1], // Remove quotes
				quoted:          true,
				naturalLanguage: false,
				raw:             arg,
			})
		} else {
			// Check if this is a multi-word natural language pattern that should be split
			if shouldSplitNaturalLanguage(arg) {
				// Split on whitespace and add as separate tokens
				words := strings.Fields(arg)
				for _, word := range words {
					tokens = append(tokens, axToken{
						value:           word,
						quoted:          false,
						naturalLanguage: true, // Mark as natural language split
						raw:             word,
					})
				}
			} else {
				tokens = append(tokens, axToken{
					value:           arg,
					quoted:          false,
					naturalLanguage: false,
					raw:             arg,
				})
			}
		}
	}

	return tokens
}

// shouldSplitNaturalLanguage determines if a multi-word argument should be split into tokens
func shouldSplitNaturalLanguage(arg string) bool {
	// Only split if it contains spaces (indicating multiple words)
	if !strings.Contains(arg, " ") {
		return false
	}

	// Split into words and check if first word is a natural language keyword
	words := strings.Fields(arg)
	if len(words) < 2 {
		return false
	}

	firstWord := strings.ToLower(words[0])

	// Special handling for grammatical connectors using map lookup
	if grammaticalMap[firstWord] {
		// Check if phrase contains context keywords - if so, keep together
		for i := 1; i < len(words); i++ {
			if isContextKeyword(words[i]) {
				return false // Keep together for semantic parsing
			}
		}
		// If no context keywords, follow normal predicate splitting logic
		return shouldSplitPredicatePhrase(words)
	}

	// Check against all splitting keywords using map lookups
	// These keywords should trigger splitting when they start a multi-word phrase
	if naturalPredicateMap[firstWord] ||
		contextTransitMap[firstWord] ||
		actorTransitMap[firstWord] ||
		temporalMap[firstWord] ||
		soActionMap[firstWord] ||
		firstWord == "has" || firstWord == "have" {
		return true
	}

	return false
}

// shouldSplitPredicatePhrase determines if a phrase starting with "is"/"are" should be split
// Split if it contains context keywords OR if it's a complex multi-word attribute
func shouldSplitPredicatePhrase(words []string) bool {
	if len(words) < 2 {
		return false
	}

	// Check if any of the remaining words are context keywords using map lookup
	for i := 1; i < len(words); i++ {
		if isContextKeyword(words[i]) {
			return true
		}
	}

	// Check if it's a complex multi-word attribute (3+ words after "is"/"are")
	// Complex attributes like "primary care physician" should be split for better search
	if len(words) >= 4 { // "is" + 3 or more attribute words
		return true
	}

	// If it's a 2-word simple phrase like "has certification", keep as single predicate
	// If no context keywords found, treat as single predicate phrase
	return false
}

// parsePredicateWithContexts handles phrases like "has diagnosis of CONDITION_X"
// Returns the meaningful predicate (diagnosis) and contexts (CONDITION_X) separately
func parsePredicateWithContexts(phrase string) (predicate string, contexts []string) {
	words := strings.Fields(phrase)
	if len(words) < 2 {
		return "", nil
	}

	// Skip grammatical connectors using map lookup
	startIdx := 0
	if grammaticalMap[strings.ToLower(words[0])] {
		startIdx = 1
	}

	if startIdx >= len(words) {
		return "", nil
	}

	var predicateParts []string
	var currentContexts []string
	inContextMode := false

	for i := startIdx; i < len(words); i++ {
		word := strings.ToLower(words[i])

		// Check if this is a context keyword using map lookup
		if isContextKeyword(word) {
			// Skip the keyword itself, switch to context mode
			inContextMode = true
			continue
		}

		if inContextMode {
			// After context keyword, add to contexts
			currentContexts = append(currentContexts, word)
		} else {
			// Before any context keyword, build the predicate
			predicateParts = append(predicateParts, word)
		}
	}

	if len(predicateParts) > 0 {
		predicate = strings.Join(predicateParts, " ")
	}

	return predicate, currentContexts
}

// handleInitialToken processes the first token for special cases (natural predicates, attributes, "over")
// Returns the initial state and whether position was advanced
func (p *axParser) handleInitialToken(filter *types.AxFilter) parseState {
	if len(p.tokens) == 0 {
		return stateSubjects
	}

	firstToken := strings.ToLower(p.tokens[0].value)

	// Handle "over" temporal comparison at the beginning
	if firstToken == "over" && !p.tokens[0].quoted {
		if len(p.tokens) > 1 {
			overFilter, err := p.parseOverComparison(p.tokens[1].value)
			if err != nil {
				p.addWarning(fmt.Sprintf("Invalid 'over' expression: %v", err))
			} else {
				filter.OverComparison = overFilter
			}
			p.position = 2 // Skip "over" and the value
		} else {
			p.addWarning("'over' keyword requires a value (e.g., 'over 5y')")
			p.position = 1
		}
		return stateSubjects
	}

	// Check for natural predicates using map lookup
	if naturalPredicateMap[firstToken] && !p.tokens[0].quoted {
		return statePredicates
	}

	return stateSubjects
}

// handleGrammaticalToken processes "is"/"are" tokens with natural language predicate extraction
// Returns the new state and whether the token was fully handled
func (p *axParser) handleGrammaticalToken(token axToken, filter *types.AxFilter) (parseState, bool) {
	// Check if this token came from natural language splitting
	if token.naturalLanguage {
		p.extractPredicateFromNaturalLanguage(token, filter)
		return stateContexts, true
	}

	// Check for multi-word phrase that might need splitting
	if strings.Contains(token.value, " ") {
		p.extractPredicateFromMultiWord(token, filter)
		return stateContexts, true
	}

	// Traditional behavior: "is" transitions to predicates, next token becomes predicate
	return statePredicates, true
}

// extractPredicateFromNaturalLanguage handles natural language tokens with semantic parsing
func (p *axParser) extractPredicateFromNaturalLanguage(token axToken, filter *types.AxFilter) {
	if strings.Contains(token.value, " ") {
		words := strings.Fields(token.value)
		if shouldSplitPredicatePhrase(words) {
			// Parse complex phrase with context keywords
			predicate, contexts := parsePredicateWithContexts(token.value)
			filter.Predicates = append(filter.Predicates, predicate)
			filter.Contexts = append(filter.Contexts, contexts...)
		} else {
			// Simple predicate - extract meaningful part
			predicate, _ := parsePredicateWithContexts(token.value)
			if predicate != "" {
				filter.Predicates = append(filter.Predicates, predicate)
			} else {
				// Fallback to lowercase token if parsing fails
				filter.Predicates = append(filter.Predicates, strings.ToLower(token.value))
			}
		}
	} else {
		// Single word natural language token
		filter.Predicates = append(filter.Predicates, strings.ToLower(token.value))
	}
}

// extractPredicateFromMultiWord handles multi-word predicate phrases
func (p *axParser) extractPredicateFromMultiWord(token axToken, filter *types.AxFilter) {
	words := strings.Fields(token.value)
	if shouldSplitPredicatePhrase(words) {
		// Parse the complex phrase with context keywords
		predicate, contexts := parsePredicateWithContexts(token.value)
		filter.Predicates = append(filter.Predicates, predicate)
		filter.Contexts = append(filter.Contexts, contexts...)
	} else {
		// Simple predicate (like "has certification") - extract meaningful part
		predicate, _ := parsePredicateWithContexts(token.value)
		if predicate != "" {
			filter.Predicates = append(filter.Predicates, predicate)
		} else {
			// Fallback to full phrase if parsing fails
			filter.Predicates = append(filter.Predicates, strings.ToLower(token.value))
		}
	}
}

// handleOverKeyword processes the "over" temporal comparison keyword
// Returns true if the keyword was handled, and the number of positions to advance
func (p *axParser) handleOverKeyword(filter *types.AxFilter) int {
	if p.position+1 < len(p.tokens) {
		valueToken := p.tokens[p.position+1].value
		overFilter, err := p.parseOverComparison(valueToken)
		if err != nil {
			p.addWarning(fmt.Sprintf("Invalid 'over' expression: %v", err))
		} else {
			filter.OverComparison = overFilter
		}
		return 2 // Skip "over" and the value token
	}
	p.addWarning("'over' keyword requires a value (e.g., 'over 5y')")
	return 1
}

// handleTemporalKeyword processes temporal keywords (since, until, on, between)
// Returns the number of positions to advance
func (p *axParser) handleTemporalKeyword(filter *types.AxFilter) int {
	temporalTokens, consumed := p.parseTemporalWithLookahead()
	if err := p.parseTemporalSegment(temporalTokens, filter); err != nil {
		// Format error respecting the parser's error context (terminal vs plain)
		errMsg := err.Error()
		if pe, ok := err.(*ParseError); ok {
			errMsg = pe.FormatError(p.errorContext)
		}
		p.addWarning(fmt.Sprintf("Failed to parse temporal expression: %s", errMsg))
	}
	return consumed
}

// isGrammaticalToken checks if a token value is a grammatical connector
func isGrammaticalToken(value string) bool {
	lower := strings.ToLower(value)
	return grammaticalMap[lower] || strings.HasPrefix(lower, "is ") || strings.HasPrefix(lower, "are ")
}

// handleKeywordTransition processes keyword-triggered state transitions
// Returns (newState, positionDelta, handled) where handled indicates if keyword was processed
func (p *axParser) handleKeywordTransition(kwType keywordType, lowerToken string, state parseState, currentTokens []string, filter *types.AxFilter) (parseState, int, bool) {
	switch kwType {
	case keywordContextTransit:
		if err := p.commitSegment(state, currentTokens, filter); err != nil {
			p.addWarning(fmt.Sprintf("Failed to process predicates: %v", err))
		}
		return stateContexts, 1, true

	case keywordActorTransit:
		if err := p.commitSegment(state, currentTokens, filter); err != nil {
			p.addWarning(fmt.Sprintf("Failed to process contexts: %v", err))
		}
		return stateActors, 1, true

	case keywordTemporal:
		if err := p.commitSegment(state, currentTokens, filter); err != nil {
			p.addWarning(fmt.Sprintf("Failed to process current segment: %v", err))
		}
		// Special handling for "over" - numeric comparison
		if lowerToken == "over" {
			return stateSubjects, p.handleOverKeyword(filter), true
		}
		// Handle other temporal keywords (since, until, on, between)
		return stateSubjects, p.handleTemporalKeyword(filter), true

	case keywordSoAction:
		if err := p.commitSegment(state, currentTokens, filter); err != nil {
			p.addWarning(fmt.Sprintf("Failed to process current segment: %v", err))
		}
		return stateSo, 1, true
	}

	return state, 0, false
}

func (p *axParser) parse(filter *types.AxFilter) (*types.AxFilter, error) {
	// Initialize state using helper for first token handling
	state := p.handleInitialToken(filter)
	currentTokens := []string{}

	for p.position < len(p.tokens) {
		token := p.tokens[p.position]
		lowerToken := strings.ToLower(token.value)

		// Handle grammatical connectors (is/are) with semantic predicate extraction
		if isGrammaticalToken(token.value) {
			if err := p.commitSegment(state, currentTokens, filter); err != nil {
				p.addWarning(fmt.Sprintf("Failed to process subjects: %v", err))
			}
			state, _ = p.handleGrammaticalToken(token, filter)
			currentTokens = []string{}
			p.position++
			continue
		}

		// Keywords take precedence unless quoted (ambiguity resolution)
		if !token.quoted {
			kwType := classifyKeyword(lowerToken)
			if newState, posDelta, handled := p.handleKeywordTransition(kwType, lowerToken, state, currentTokens, filter); handled {
				state = newState
				currentTokens = []string{}
				p.position += posDelta
				continue
			}
		}

		// Regular token - add to current segment
		currentTokens = append(currentTokens, token.value)
		p.position++
	}

	// Commit final segment
	if len(currentTokens) > 0 {
		if err := p.commitSegment(state, currentTokens, filter); err != nil {
			p.addWarning(fmt.Sprintf("Failed to process final segment: %v", err))
		}
	}

	// Validation with warnings for potential issues
	p.validateFilter(filter)

	// If we have warnings, include them in a special error type (best-effort parsing)
	if len(p.warnings) > 0 {
		return filter, &ParseWarning{
			Filter:   filter,
			Warnings: p.warnings,
		}
	}

	return filter, nil
}

func (p *axParser) parseTemporalWithLookahead() ([]string, int) {
	if p.position >= len(p.tokens) {
		return []string{}, 0
	}

	temporalKeyword := p.tokens[p.position].value
	consumed := 1
	temporalTokens := []string{temporalKeyword}

	// Lookahead to determine temporal expression boundaries
	for i := p.position + 1; i < len(p.tokens); i++ {
		token := p.tokens[i]

		// Stop if we hit another keyword (unless quoted)
		if !token.quoted {
			if IsAxKeyword(token.value) {
				break
			}
		}

		// For "between X and Y" pattern, include "and" as part of temporal
		if strings.ToLower(token.value) == "and" && strings.ToLower(temporalKeyword) == "between" {
			temporalTokens = append(temporalTokens, token.value)
			consumed++
			continue
		}

		// Check if this looks like a temporal expression continuation
		if IsTemporalContinuation(token.value) {
			temporalTokens = append(temporalTokens, token.value)
			consumed++
		} else {
			// Use heuristic: if next token after this is a keyword, include this
			if i+1 < len(p.tokens) && !p.tokens[i+1].quoted && IsAxKeyword(p.tokens[i+1].value) {
				temporalTokens = append(temporalTokens, token.value)
				consumed++
				break
			}
			// Otherwise, include this token and stop
			temporalTokens = append(temporalTokens, token.value)
			consumed++
			break
		}
	}

	// Fallback: consuming everything after temporal keyword if lookahead fails
	if len(temporalTokens) == 1 && p.position+1 < len(p.tokens) {
		for i := p.position + 1; i < len(p.tokens); i++ {
			temporalTokens = append(temporalTokens, p.tokens[i].value)
			consumed++
		}
	}

	return temporalTokens, consumed
}

// IsAxKeyword checks if a value is an ax grammar keyword
// Used for lookahead to determine when to stop parsing temporal expressions
func IsAxKeyword(value string) bool {
	keywords := []string{"is", "are", "of", "from", "by", "via", "since", "until", "on", "between", "so", "therefore"}
	lower := strings.ToLower(value)
	for _, keyword := range keywords {
		if lower == keyword {
			return true
		}
	}
	return false
}

func (p *axParser) commitSegment(state parseState, tokens []string, filter *types.AxFilter) error {
	if len(tokens) == 0 {
		return nil
	}

	switch state {
	case stateSubjects:
		// Normalize subjects to uppercase for consistent storage/display
		// Case-agnostic ID resolution will be handled in database layer
		filter.Subjects = uppercaseTokens(tokens)
	case statePredicates:
		// Preserve original case for predicates (like subjects)
		filter.Predicates = tokens
	case stateContexts:
		// Case-insensitive processing - use lowercase for consistency with database
		filter.Contexts = lowercaseTokens(tokens)
	case stateActors:
		// Space-separated actors until next keyword
		// Case-insensitive processing
		filter.Actors = lowercaseTokens(tokens)
	case stateSo:
		// 'so' actions - keep original case
		filter.SoActions = tokens
	}

	return nil
}

func (p *axParser) parseTemporalSegment(tokens []string, filter *types.AxFilter) error {
	if len(tokens) == 0 {
		return nil
	}

	keyword := strings.ToLower(tokens[0])
	expr := strings.Join(tokens[1:], " ")

	switch keyword {
	case "since":
		timeStart, err := ParseTemporalExpression(expr)
		if err != nil {
			// Use structured error with suggestions
			return p.structuredError(ErrorKindTemporal,
				"invalid 'since' expression '%s': %v", expr, err).
				WithSuggestion("Use ISO date format: 2024-01-01").
				WithSuggestion("Or relative time: 3m (3 months ago), 1y (1 year ago)").
				WithSuggestion("Or named days: yesterday, last monday")
		}
		filter.TimeStart = timeStart
	case "until":
		timeEnd, err := ParseTemporalExpression(expr)
		if err != nil {
			return p.structuredError(ErrorKindTemporal,
				"invalid 'until' expression '%s': %v", expr, err).
				WithSuggestion("Use ISO date format: 2024-12-31").
				WithSuggestion("Or relative time: 3m (3 months from now), 1y (1 year from now)").
				WithSuggestion("Or named days: tomorrow, next friday")
		}
		filter.TimeEnd = timeEnd
	case "on":
		timePoint, err := ParseTemporalExpression(expr)
		if err != nil {
			return p.structuredError(ErrorKindTemporal,
				"invalid 'on' expression '%s': %v", expr, err).
				WithSuggestion("Use ISO date format: 2024-06-15").
				WithSuggestion("Or named days: today, yesterday, monday").
				WithSuggestion("'on' specifies a specific date (entire day)")
		}
		// "on" means both start and end of that day
		startOfDay := time.Date(timePoint.Year(), timePoint.Month(), timePoint.Day(), 0, 0, 0, 0, timePoint.Location())
		endOfDay := startOfDay.Add(24 * time.Hour)
		filter.TimeStart = &startOfDay
		filter.TimeEnd = &endOfDay
	case "between":
		// Handle "between X and Y" format
		parts := strings.Split(expr, " and ")
		if len(parts) != 2 {
			return p.structuredError(ErrorKindSyntax,
				"'between' requires format 'between X and Y', got: %s", expr).
				WithSuggestion("Use format: between 2024-01-01 and 2024-12-31").
				WithSuggestion("Or: between 6m and 3m (6 months ago to 3 months ago)").
				WithSuggestion("Or: between last monday and yesterday")
		}
		timeStart, err := ParseTemporalExpression(parts[0])
		if err != nil {
			return p.structuredError(ErrorKindTemporal,
				"invalid start time in 'between': %v", err).
				WithSuggestion("Use ISO date format: 2024-01-01").
				WithSuggestion("Or relative time: 6m (6 months ago)").
				WithSuggestion("Or named days: last monday, yesterday")
		}
		timeEnd, err := ParseTemporalExpression(parts[1])
		if err != nil {
			return p.structuredError(ErrorKindTemporal,
				"invalid end time in 'between': %v", err).
				WithSuggestion("Use ISO date format: 2024-12-31").
				WithSuggestion("Or relative time: 3m (3 months ago)").
				WithSuggestion("Or named days: yesterday, today")
		}
		filter.TimeStart = timeStart
		filter.TimeEnd = timeEnd
	}

	return nil
}

func (p *axParser) validateFilter(filter *types.AxFilter) {
	// Validation with warnings for potential issues
	if len(filter.Subjects) == 0 && len(filter.Predicates) == 0 &&
		len(filter.Contexts) == 0 && len(filter.Actors) == 0 &&
		filter.TimeStart == nil && filter.TimeEnd == nil && filter.OverComparison == nil {
		p.addWarning("Empty query may return large result set - consider adding filters")
	}

	if filter.TimeStart != nil && filter.TimeEnd != nil && filter.TimeStart.After(*filter.TimeEnd) {
		p.addWarning("Start time is after end time - this may return no results")
	}

	if filter.Limit > 1000 {
		p.addWarning(fmt.Sprintf("Large limit (%d) may impact performance", filter.Limit))
	}
}

func (p *axParser) addWarning(warning string) {
	p.warnings = append(p.warnings, warning)
}

// Utility functions

func uppercaseTokens(tokens []string) []string {
	result := make([]string, len(tokens))
	for i, token := range tokens {
		result[i] = strings.ToUpper(token)
	}
	return result
}

func lowercaseTokens(tokens []string) []string {
	result := make([]string, len(tokens))
	for i, token := range tokens {
		result[i] = strings.ToLower(token)
	}
	return result
}

// ParseWarning represents best-effort parsing with warnings
type ParseWarning struct {
	Filter   *types.AxFilter
	Warnings []string
}

func (pw *ParseWarning) Error() string {
	return fmt.Sprintf("Parsed with warnings: %s", strings.Join(pw.Warnings, "; "))
}

// GetWarnings extracts warnings from error if it's a ParseWarning
func GetWarnings(err error) []string {
	if pw, ok := err.(*ParseWarning); ok {
		return pw.Warnings
	}
	return nil
}

// parseOverComparison parses "over" comparison values like "5y" or "6m"
func (p *axParser) parseOverComparison(value string) (*types.OverFilter, error) {
	// Parse patterns like "5y", "10.5y", "6m"
	if len(value) < 2 {
		return nil, p.contextualError("invalid 'over' format '%s', expected format like '5y' or '6m'", value)
	}

	// Extract numeric part and unit
	numPart := value[:len(value)-1]
	unit := value[len(value)-1:]

	// Validate unit
	if unit != "y" && unit != "m" {
		// If no unit provided, suggest adding 'y'
		if _, err := parseFloat(value); err == nil {
			return nil, p.contextualError("missing unit in '%s' for 'over' expression, did you mean '%sy'?", value, value)
		}
		return nil, p.contextualError("unsupported unit '%s' in 'over' expression, use 'y' for years or 'm' for months", unit)
	}

	// Parse numeric value
	numValue, err := parseFloat(numPart)
	if err != nil {
		return nil, p.contextualError("invalid numeric value '%s' in 'over' expression: %v", numPart, err)
	}

	return &types.OverFilter{
		Value:    numValue,
		Unit:     unit,
		Operator: "over", // Currently only support "over" (>=)
	}, nil
}

// parseFloat is a helper to parse float values
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
