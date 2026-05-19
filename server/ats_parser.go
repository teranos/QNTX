package server

// ATS code parsing for scheduled job creation.
// Parses ATS block syntax and extracts handler name, payload, and source URL.
//
// TODO(plugin-pulse-integration): Re-enable ix command parsing via pluggable ATS parsers.
// Currently disabled to remove hardcoded domain-specific logic.

import (
	"strings"

	"github.com/teranos/errors"
)

// ParsedATSCode contains the pre-computed values for a scheduled job
type ParsedATSCode struct {
	// HandlerName is the async handler to invoke (e.g., "python.script")
	HandlerName string
	// Payload is the pre-computed JSON payload for the handler
	Payload []byte
	// SourceURL is used for deduplication
	SourceURL string
}

// ParseATSCodeWithForce parses an ATS code string and extracts handler, payload, and source URL.
// The jobID is used for generating unique identifiers if needed.
// The force flag indicates a one-time execution (affects some behaviors).
//
// TODO(plugin-pulse-integration): Currently returns error for all ix commands.
// Will be re-enabled when pluggable ATS parsers are implemented.
func ParseATSCodeWithForce(atsCode string, jobID string, force bool) (*ParsedATSCode, error) {
	atsCode = strings.TrimSpace(atsCode)
	if atsCode == "" {
		return nil, errors.New("empty ATS code")
	}

	// Tokenize the ATS code
	tokens := tokenizeATSCode(atsCode)
	if len(tokens) == 0 {
		return nil, errors.New("empty ATS code")
	}

	// Route based on first token (command)
	switch tokens[0] {
	case "ix":
		return parseIxCommand(tokens[1:], jobID)
	default:
		return nil, errors.Newf("unknown command: %s (supported: ix)", tokens[0])
	}
}

// parseIxCommand handles "ix <subcommand> <args...>" syntax.
// TODO(plugin-pulse-integration): Re-enable via pluggable ATS parsers.
func parseIxCommand(tokens []string, jobID string) (*ParsedATSCode, error) {
	if len(tokens) == 0 {
		return nil, errors.New("ix command requires a subcommand")
	}

	// TODO(plugin-pulse-integration): Re-enable via pluggable ATS parsers.
	// All ix subcommands temporarily disabled to remove hardcoded domain logic.
	return nil, errors.New("ix command temporarily disabled - plugin-pulse integration in progress")
}

// tokenizeATSCode splits ATS code into tokens, respecting quotes
func tokenizeATSCode(code string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false
	quoteChar := rune(0)

	for _, ch := range code {
		switch {
		case ch == '"' || ch == '\'':
			if inQuotes && ch == quoteChar {
				// End of quoted string
				inQuotes = false
				quoteChar = 0
			} else if !inQuotes {
				// Start of quoted string
				inQuotes = true
				quoteChar = ch
			} else {
				// Different quote inside quotes
				current.WriteRune(ch)
			}
		case ch == ' ' || ch == '\t' || ch == '\n':
			if inQuotes {
				current.WriteRune(ch)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
