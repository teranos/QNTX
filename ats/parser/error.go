package parser

import (
	"fmt"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

// ErrorSeverity indicates the severity level of a parser error
type ErrorSeverity string

const (
	SeverityError   ErrorSeverity = "error"   // Syntax/semantic errors that prevent parsing
	SeverityWarning ErrorSeverity = "warning" // Best-effort parsing warnings
	SeverityInfo    ErrorSeverity = "info"    // Informational messages
	SeverityHint    ErrorSeverity = "hint"    // Suggestions for improvement
)

// ErrorKind categorizes parser errors for programmatic handling
type ErrorKind string

const (
	ErrorKindSyntax   ErrorKind = "syntax"   // Invalid syntax (malformed query)
	ErrorKindSemantic ErrorKind = "semantic" // Semantically invalid
	ErrorKindTemporal ErrorKind = "temporal" // Temporal expression error
	ErrorKindContext  ErrorKind = "context"  // Context/state error
	ErrorKindUnknown  ErrorKind = "unknown"  // Uncategorized
)

// ParseError represents a structured parser error with metadata
type ParseError struct {
	Err         error                  // Underlying error
	Kind        ErrorKind              // Error category
	Severity    ErrorSeverity          // Error severity
	Message     string                 // Human-readable message
	Position    int                    // Token position where error occurred
	TokenCount  int                    // Total tokens being parsed
	Token       *axToken               // Token that caused the error (optional)
	Range       *Range                 // Source range (optional)
	Suggestions []string               // Possible fixes
	Context     map[string]interface{} // Additional debug context
	Timestamp   time.Time              // When error occurred
}

// Error implements error interface
func (e *ParseError) Error() string {
	return e.FormatError(ErrorContextTerminal)
}

// FormatError generates context-appropriate error message
func (e *ParseError) FormatError(ctx ErrorContext) string {
	if ctx == ErrorContextPlain {
		return e.formatPlainError()
	}
	return e.formatTerminalError()
}

// formatPlainError creates concise error for web UI/logs
func (e *ParseError) formatPlainError() string {
	msg := e.Message
	if e.Position >= 0 && e.TokenCount > 0 {
		msg += fmt.Sprintf(" (at position %d/%d)", e.Position, e.TokenCount)
	}
	if len(e.Suggestions) > 0 {
		msg += fmt.Sprintf(". Suggestions: %s", strings.Join(e.Suggestions, ", "))
	}
	return msg
}

// formatTerminalError creates rich colored error for terminal
func (e *ParseError) formatTerminalError() string {
	// Base message with color based on severity
	var baseMsg string
	switch e.Severity {
	case SeverityError:
		baseMsg = pterm.Red(e.Message)
	case SeverityWarning:
		baseMsg = pterm.Yellow(e.Message)
	case SeverityInfo:
		baseMsg = pterm.Blue(e.Message)
	case SeverityHint:
		baseMsg = pterm.LightCyan(e.Message)
	default:
		baseMsg = e.Message
	}

	// Add context information
	context := fmt.Sprintf("\n\n%s", pterm.LightCyan("Context:"))
	if e.Position >= 0 && e.TokenCount > 0 {
		context += fmt.Sprintf("\n  %s %d/%d", pterm.Yellow("Position:"), e.Position, e.TokenCount)
	}
	if e.Token != nil {
		context += fmt.Sprintf("\n  %s '%s'", pterm.Yellow("Token:"), e.Token.value)
	}

	// Add suggestions
	if len(e.Suggestions) > 0 {
		context += fmt.Sprintf("\n\n%s", pterm.Green("Suggestions:"))
		for _, suggestion := range e.Suggestions {
			context += fmt.Sprintf("\n  • %s", suggestion)
		}
	}

	return fmt.Sprintf("%s%s", baseMsg, context)
}

// Unwrap for errors.Is/As compatibility
func (e *ParseError) Unwrap() error {
	return e.Err
}

// IsWarning returns true if this is a warning (not an error)
func (e *ParseError) IsWarning() bool {
	return e.Severity == SeverityWarning
}

// Builder pattern for constructing ParseErrors

// NewParseError creates a new ParseError with the given kind and message
func NewParseError(kind ErrorKind, message string) *ParseError {
	return &ParseError{
		Kind:      kind,
		Severity:  SeverityError,
		Message:   message,
		Position:  -1,
		Context:   make(map[string]interface{}),
		Timestamp: time.Now(),
	}
}

// WithPosition sets the token position where the error occurred
func (e *ParseError) WithPosition(pos int, total int) *ParseError {
	e.Position = pos
	e.TokenCount = total
	return e
}

// WithToken sets the token that caused the error
func (e *ParseError) WithToken(token *axToken) *ParseError {
	e.Token = token
	return e
}

// WithRange sets the source range
func (e *ParseError) WithRange(r Range) *ParseError {
	e.Range = &r
	return e
}

// WithSeverity sets the error severity
func (e *ParseError) WithSeverity(sev ErrorSeverity) *ParseError {
	e.Severity = sev
	return e
}

// WithSuggestion adds a suggestion for fixing the error
func (e *ParseError) WithSuggestion(suggestion string) *ParseError {
	e.Suggestions = append(e.Suggestions, suggestion)
	return e
}

// WithContext adds debug context metadata
func (e *ParseError) WithContext(key string, value interface{}) *ParseError {
	e.Context[key] = value
	return e
}

// WithUnderlying sets the underlying error
func (e *ParseError) WithUnderlying(err error) *ParseError {
	e.Err = err
	return e
}
