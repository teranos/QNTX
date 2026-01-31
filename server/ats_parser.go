package server

// ATS code parsing for scheduled job creation.
// Parses ATS block syntax and extracts handler name, payload, and source URL.
//
// Supported syntax:
//   ix <git-url>                       - Auto-detect git repo URL and ingest
//   ix <git-url> --since last_run      - Incremental ingestion since last run
//   ix <git-url> --no-deps             - Skip dependency ingestion
//   ix git <url>                       - Explicit git subcommand (same as above)

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/errors"
	ixgit "github.com/teranos/QNTX/qntx-code/ixgest/git"
)

// ParsedATSCode contains the pre-computed values for a scheduled job
type ParsedATSCode struct {
	// HandlerName is the async handler to invoke (e.g., "ixgest.git")
	HandlerName string
	// Payload is the pre-computed JSON payload for the handler
	Payload []byte
	// SourceURL is used for deduplication (e.g., the git repo URL)
	SourceURL string
}

// ParseATSCodeWithForce parses an ATS code string and extracts handler, payload, and source URL.
// The jobID is used for generating unique identifiers if needed.
// The force flag indicates a one-time execution (affects some behaviors).
//
// Supported syntax:
//   - ix <url>                        - Auto-detect and ingest git repository
//   - ix <url> --since last_run       - Incremental ingestion since last run
//   - ix <url> --no-deps              - Skip dependency ingestion
//   - ix git <url>                    - Explicit git subcommand (same as above)
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
		err := errors.Newf("unknown command: %s (supported: ix)", tokens[0])
		return nil, errors.WithDetail(err, fmt.Sprintf("ATS code: %s", atsCode))
	}
}

// parseIxCommand handles "ix <subcommand|url> <args...>" syntax
// Supports both explicit subcommands (ix git <url>) and auto-detection (ix <url>)
func parseIxCommand(tokens []string, jobID string) (*ParsedATSCode, error) {
	if len(tokens) == 0 {
		return nil, errors.New("ix command requires a target (e.g., ix https://github.com/user/repo)")
	}

	// Check for explicit subcommand
	switch tokens[0] {
	case "git":
		return parseIxGitCommand(tokens[1:], jobID)
	default:
		// Auto-detect: if the first token looks like a git URL, treat as ix git <url>
		if ixgit.IsRepoURL(tokens[0]) {
			return parseIxGitCommand(tokens, jobID)
		}
		err := errors.Newf("unknown ix target: %s (expected a git repository URL)", tokens[0])
		return nil, errors.WithDetail(err, fmt.Sprintf("Provided: %s", tokens[0]))
	}
}

// parseIxGitCommand handles "ix git <url> [options]" syntax
func parseIxGitCommand(tokens []string, jobID string) (*ParsedATSCode, error) {
	if len(tokens) == 0 {
		return nil, errors.New("ix git requires a repository URL or path")
	}

	// First non-flag token is the repository URL/path
	repoURL := ""
	since := ""
	noDeps := false

	i := 0
	for i < len(tokens) {
		token := tokens[i]

		if strings.HasPrefix(token, "--") {
			// Parse flags
			switch token {
			case "--since":
				if i+1 >= len(tokens) {
					return nil, errors.New("--since requires a value")
				}
				i++
				since = tokens[i]
			case "--no-deps":
				noDeps = true
			default:
				return nil, errors.Newf("unknown flag: %s", token)
			}
		} else if repoURL == "" {
			// First non-flag is the repo URL
			repoURL = token
		} else {
			return nil, errors.Newf("unexpected argument: %s", token)
		}
		i++
	}

	if repoURL == "" {
		return nil, errors.New("ix git requires a repository URL or path")
	}

	// Build payload for ixgest.git handler
	payload := ixgit.GitIngestionPayload{
		RepositorySource: repoURL,
		Actor:            fmt.Sprintf("scheduled:%s", jobID),
		Verbosity:        0,
		NoDeps:           noDeps,
		Since:            since, // Will be resolved at execution time if "last_run"
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal payload")
	}

	// Handler availability is validated in pulse_schedules.go after parsing.
	// If "ixgest.git" handler is not registered (e.g., qntx-code plugin disabled),
	// job creation fails with early feedback instead of runtime error.
	return &ParsedATSCode{
		HandlerName: "ixgest.git",
		Payload:     payloadJSON,
		SourceURL:   repoURL,
	}, nil
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
