package parser

import (
	"fmt"
	"strings"

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
	return parseAxQueryDispatch(args, verbosity, ctx)
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

// Utility functions

func uppercaseTokens(tokens []string) []string {
	if tokens == nil {
		return nil
	}
	result := make([]string, len(tokens))
	for i, token := range tokens {
		result[i] = strings.ToUpper(token)
	}
	return result
}

func lowercaseTokens(tokens []string) []string {
	if tokens == nil {
		return nil
	}
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

