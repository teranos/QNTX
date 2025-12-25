# Verbosity Levels in qntx

This guide explains the progressive verbosity pattern used across qntx commands. The `-v` flag can be repeated to increase output detail, providing users with control over how much information they see.

## Overview

All qntx commands support a standard verbosity pattern:

```bash
qntx <command>        # Level 0 (default) - Clean output
qntx <command> -v     # Level 1 - Parse details and progress
qntx <command> -vv    # Level 2 - Debug information
qntx <command> -vvv   # Level 3 - Deep debugging context
qntx <command> -vvvv  # Level 4 - Ultra-verbose troubleshooting
```

The verbosity flag is defined globally in the root command and available to all subcommands.

## Level Definitions

### Level 0 (Default - No Flags)

**Purpose:** Clean, user-facing output only

**What you see:**
- Final results
- Critical errors
- Essential progress messages

**What's hidden:**
- Parsing details
- Internal processing steps
- SQL queries
- Debug information

**Example (ax command):**
```
Subject: "candidate_123"
Predicate: "has_skill"
Object: "Go"

1 attestation found
```

### Level 1 (`-v`)

**Purpose:** Parse details, warnings, and basic progress

**Additional output:**
- Parsing breakdowns (subjects, predicates, objects)
- Execution context
- Warnings that don't stop execution
- Basic progress indicators
- Query construction details

**Example (ax command):**
```
Parsing query...
  Subject: "candidate_123"
  Predicate: "has_skill"
  Object: "Go"
  Context: (none)
  Actor: user@example.com

Executing query...
Found 1 matching attestation

Results:
  [attestation details...]
```

**Example (event command):**
```
Processing event "Tech Conference 2025"
Finding personas... 3 found
Matching contacts... 15 candidates evaluated
Applying score threshold 0.5... 8 contacts matched
```

### Level 2 (`-vv`)

**Purpose:** Debug information for troubleshooting

**Additional output:**
- SQL queries executed
- Database operations
- Detailed execution timing
- Trace information
- Full result sets (including sameness analysis)
- File paths and configuration

**Example (ax command):**
```
Parsing query...
[Full parse breakdown...]

Executing query...
SQL: SELECT * FROM attestations WHERE subject = ? AND predicate = ? AND object = ?
Parameters: ["candidate_123", "has_skill", "Go"]
Execution time: 2.3ms

Results with full context:
  [Complete attestation data with all metadata...]

Sameness Analysis:
  Similar attestations: 3 found
  [Detailed similarity scores...]
```

**Example (graph command):**
```
2025-11-14T14:17:52.836+0100  INFO  graph.server  Graph server starting  {"port": 877, "verbosity": 2, "level_name": "Debug (-vv)"}
QNTX Server
URL: http://localhost:877
Verbosity: Debug (-vv)
Logs: tmp/graph-debug.log

[WebSocket log panel shows detailed query processing...]
```

**BCS module:**
- Includes trace information when `verbosity >= 2`
- Shows data transformation steps
- Displays validation details

### Level 3 (`-vvv`)

**Purpose:** Deep debugging with extensive context

**Additional output:**
- Full Cobra command usage information
- Complete Go error details (not just error messages)
- Stack traces where applicable
- All internal state transitions
- Detailed matching scores and explanations

**Example (ax command):**
```
[Full Cobra command structure...]
[Complete parse tree...]
[All SQL queries with full result sets...]
[Go error details with types and values...]

Matching algorithm details:
  Candidate A: score=0.85 (AI=0.9, recency=0.8, keywords=0.7, warmth=0.9, timezone=1.0)
  Reasoning: [Detailed explanation of score calculation...]
```

**Event subsystem:**
- Detailed matching scores for each contact
- Full explanation of score components
- Reasoning for inclusions/exclusions

### Level 4 (`-vvvv`)

**Purpose:** Ultra-verbose for troubleshooting complex issues

**Additional output:**
- Raw SQL statements before parameter binding
- Database query planning details
- Complete HTTP request/response bodies
- Full JSON serialization/deserialization traces
- Every internal function call

**Example (event repository):**
```
SQL Query Plan:
  Table scan: events
  Index lookup: event_id = ?
  Expected rows: 1
  Cost estimate: 1.2

Raw SQL (before binding):
  SELECT * FROM events WHERE id = ?

Bound SQL:
  SELECT * FROM events WHERE id = 123

[Full result set with all columns...]
```

## Implementation Patterns

### Extracting Verbosity in Commands

Standard pattern used across all commands:

```go
func runCommand(cmd *cobra.Command, args []string) error {
    // Extract verbosity count
    verbosity, _ := cmd.Flags().GetCount("verbose")

    // Pass to subsystems
    service := NewService(db, verbosity)

    // Use for conditional output
    if verbosity >= 2 {
        fmt.Printf("Debug: executing with config: %+v\n", config)
    }

    return service.Execute()
}
```

### Integration with Display Package

The display package receives verbosity as part of options:

```go
opts := display.Options{
    Audience:  display.AudienceHuman,
    Level:     display.LevelNormal,
    Verbosity: &verbosity,  // Pointer to verbosity count
    OutFormat: "table",
    Writer:    os.Stdout,
}

renderer := display.NewRenderer(opts)
```

### Integration with Structured Logging

Subsystems map verbosity to zap log levels:

```go
// From github.com/teranos/QNTX/logger/verbosity.go
func VerbosityToLevel(verbosity int) zapcore.Level {
    switch verbosity {
    case 0:
        return zapcore.WarnLevel   // Only warnings and errors
    case 1:
        return zapcore.InfoLevel   // Info and above
    case 2:
        return zapcore.DebugLevel  // Debug and above
    default:
        return zapcore.DebugLevel  // All logs at 3+
    }
}
```

### Subsystem-Specific Interpretation

While the general pattern is consistent, subsystems may interpret levels slightly differently based on their needs:

**Ax command:**
- Level 0: Results only
- Level 1: Parse breakdown
- Level 2: SQL queries and sameness analysis
- Level 3: Full Go error details and Cobra usage

**Graph command:**
- Level 0: Errors only (with toast notifications)
- Level 1: Info-level logs
- Level 2: Debug logs + file logging (tmp/graph-debug.log)
- Level 3+: Trace-level logs

**BCS module:**
- Level 0-1: Basic processing
- Level 2+: Includes trace information in output

## Practical Examples

### Debugging a Query Issue

Start with default output, progressively increase verbosity:

```bash
# Level 0 - See the problem
qntx ax "is engineer"
# Output: 0 results (unexpected!)

# Level 1 - See how query was parsed
qntx ax "is engineer" -v
# Output shows: Subject="is", Predicate="engineer" (wrong!)

# Level 2 - See the SQL query
qntx ax "is engineer" -vv
# SQL: SELECT * FROM attestations WHERE subject = 'is' AND predicate = 'engineer'
# Ah! The query parsing is incorrect for this syntax

# Level 3 - See complete parse details
qntx ax "is engineer" -vvv
# [Full parse tree shows the parsing logic...]
```

### Performance Investigation

Use level 2 to see timing and queries:

```bash
qntx event match 123 -vv
# Shows:
#   SQL query execution times
#   Number of candidates processed
#   Matching algorithm timing
#   Database operation details
```

### Development and Testing

Use level 4 during development to see everything:

```bash
qntx server -vvvv
# Ultra-verbose output shows:
#   Every WebSocket message
#   All query transformations
#   Complete graph building steps
#   Full error context
```

## Guidelines

### For Command Implementation

- Always extract verbosity: `verbosity, _ := cmd.Flags().GetCount("verbose")`
- Pass verbosity to subsystems that need it
- Use consistent level thresholds (0, 1, 2, 3, 4)
- Default to level 0 output (clean, user-facing)
- Progressive disclosure: each level adds detail without removing previous level's output

### For Users

- Start with level 0 (default)
- Add `-v` if something seems wrong
- Add `-vv` if you need to see SQL queries or debug info
- Add `-vvv` for deep debugging (bug reports)
- Add `-vvvv` only when explicitly debugging code

### For Documentation

- Document what each level shows for your command
- Provide examples showing the progression
- Mention subsystem-specific interpretations
- Link to this guide for general explanation

## Relationship to Other Output Controls

Verbosity is orthogonal to other output flags:

- `--json`: Output format (doesn't change verbosity)
- `--audience llm`: Changes message framing (not detail level)
- `--debug`: Display package debug mode (separate from verbosity levels)
- `--dry-run`: Execution mode (not output detail)

These can be combined:

```bash
qntx ax "is engineer" -vv --json
# Level 2 verbosity + JSON output format
```

## See Also

- `docs/development/error-handling.md` - How errors integrate with verbosity levels
- `docs/development/display-package.md` - Display renderer and output formatting
- `github.com/teranos/QNTX/logger/verbosity.go` - Verbosity to log level mapping
