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
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/errors"
	ixgit "github.com/teranos/QNTX/qntx-code/ixgest/git"
)

// Sentinel error for missing ingest scripts
var ErrNoIngestScript = errors.New("no ingest script found")

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
// The db parameter enables querying for dynamically-registered Python ingest scripts.
//
// Supported syntax:
//   - ix <url>                        - Auto-detect and ingest git repository
//   - ix <url> --since last_run       - Incremental ingestion since last run
//   - ix <url> --no-deps              - Skip dependency ingestion
//   - ix git <url>                    - Explicit git subcommand (same as above)
//   - ix <type> <input>               - Use Python script registered as "{type}-ingestion"
func ParseATSCodeWithForce(db *sql.DB, atsCode string, jobID string, force bool) (*ParsedATSCode, error) {
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
		return parseIxCommand(db, tokens[1:], jobID)
	default:
		return nil, errors.Newf("unknown command: %s (supported: ix)", tokens[0])
	}
}

// parseIxCommand handles "ix <subcommand|url> <args...>" syntax
// Supports explicit subcommands (ix git <url>), auto-detection (ix <url>),
// and dynamic Python script routing (ix csv <file>)
func parseIxCommand(db *sql.DB, tokens []string, jobID string) (*ParsedATSCode, error) {
	if len(tokens) == 0 {
		return nil, errors.New("ix command requires a target (e.g., ix https://github.com/user/repo)")
	}

	// Determine script type and input
	var scriptType string
	var input []string

	// Check if first token is a URL (auto-detect git)
	if ixgit.IsRepoURL(tokens[0]) {
		scriptType = "git"
		input = tokens
	} else {
		// First token is the script type (git, csv, jd, etc.)
		scriptType = tokens[0]
		input = tokens[1:]
	}

	// Query attestation store for Python script: Predicate="ix_handler", Context=scriptType
	// Attestation model: 'as python_script is ix_handler of csv by canvas_glyph at temporal'
	filters := ats.AttestationFilter{
		Predicates: []string{"ix_handler"},
		Contexts:   []string{scriptType},
		Limit:      1,
	}

	// Import storage package for GetAttestations
	attestations, err := queryAttestations(db, filters)
	if err == nil && len(attestations) > 0 {
		// Found Python script - extract code from attributes
		attestation := attestations[0]

		var attributes map[string]interface{}
		if err := json.Unmarshal([]byte(attestation.AttributesJSON), &attributes); err != nil {
			return nil, errors.Wrap(err, "failed to parse script attributes")
		}

		scriptCode, ok := attributes["code"].(string)
		if !ok || scriptCode == "" {
			return nil, errors.Newf("script '%s' has no code attribute", scriptType)
		}

		// Build payload for Python handler
		// Note: input is not passed to handler (not in scope for this PR)
		payload := map[string]interface{}{
			"script_code": scriptCode,
			"script_type": scriptType,
		}

		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Wrap(err, "failed to marshal Python handler payload")
		}

		return &ParsedATSCode{
			HandlerName: "python.script",
			Payload:     payloadJSON,
			SourceURL:   strings.Join(input, " "),
		}, nil
	}

	// No Python script found - fall back to hardcoded handlers
	if scriptType == "git" {
		return parseIxGitCommand(input, jobID)
	}

	// Return structured error with script name as detail
	noScriptErr := errors.WithDetailf(ErrNoIngestScript, "script_type=%s", scriptType)
	noScriptErr = errors.WithHintf(noScriptErr, "Click 'Create handler' button to define a Python handler for '%s'", scriptType)
	return nil, noScriptErr
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

	// TODO(Issue #356): Handler availability is not validated at job creation time.
	// This hardcodes "ixgest.git" without checking if the handler is registered.
	// If the qntx-code plugin is disabled, the job will fail at execution time with
	// "no handler registered" error. Consider validating handler availability here
	// or providing early feedback to users when creating scheduled jobs.
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

// AttestationResult represents a query result from the attestation store
type AttestationResult struct {
	ID             string
	Subjects       []string
	Predicates     []string
	Contexts       []string
	Actors         []string
	Timestamp      int64
	Source         string
	AttributesJSON string
}

// queryAttestations queries the attestation store using the storage package
func queryAttestations(db *sql.DB, filters ats.AttestationFilter) ([]AttestationResult, error) {
	// Use storage.GetAttestations which returns []*types.As
	// We need to import the storage package
	const query = `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes
		FROM attestations
		WHERE 1=1
	`

	var whereClauses []string
	var args []interface{}

	// Add predicate filter
	if len(filters.Predicates) > 0 {
		whereClauses = append(whereClauses, "EXISTS (SELECT 1 FROM json_each(predicates) WHERE value = ?)")
		args = append(args, filters.Predicates[0])
	}

	// Add context filter
	if len(filters.Contexts) > 0 {
		whereClauses = append(whereClauses, "EXISTS (SELECT 1 FROM json_each(contexts) WHERE value = ?)")
		args = append(args, filters.Contexts[0])
	}

	fullQuery := query
	if len(whereClauses) > 0 {
		fullQuery += " AND " + strings.Join(whereClauses, " AND ")
	}

	if filters.Limit > 0 {
		fullQuery += fmt.Sprintf(" LIMIT %d", filters.Limit)
	}

	rows, err := db.Query(fullQuery, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	var results []AttestationResult
	for rows.Next() {
		var r AttestationResult
		var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON string

		if err := rows.Scan(&r.ID, &subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &r.Timestamp, &r.Source, &r.AttributesJSON); err != nil {
			return nil, errors.Wrap(err, "failed to scan attestation row")
		}

		// Parse JSON arrays
		if err := json.Unmarshal([]byte(subjectsJSON), &r.Subjects); err != nil {
			return nil, errors.Wrap(err, "failed to parse subjects")
		}
		if err := json.Unmarshal([]byte(predicatesJSON), &r.Predicates); err != nil {
			return nil, errors.Wrap(err, "failed to parse predicates")
		}
		if err := json.Unmarshal([]byte(contextsJSON), &r.Contexts); err != nil {
			return nil, errors.Wrap(err, "failed to parse contexts")
		}
		if err := json.Unmarshal([]byte(actorsJSON), &r.Actors); err != nil {
			return nil, errors.Wrap(err, "failed to parse actors")
		}

		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating attestation rows")
	}

	return results, nil
}
