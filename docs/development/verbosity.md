# Verbosity Levels

**Why progressive verbosity?** Developers want different levels of detail at different times. Simple mental model: more v's = more info. LLMs can use higher verbosity for better context.

Progressive verbosity pattern for QNTX CLI commands using the `-v` flag.

## Pattern

```bash
qntx <command>        # Level 0 (default) - Results and errors only
qntx <command> -v     # Level 1 - Progress, startup, plugin status
qntx <command> -vv    # Level 2 - Queries, timing, config details
qntx <command> -vvv   # Level 3 - Plugin logs, SQL, gRPC calls
qntx <command> -vvvv  # Level 4 - Full request/response bodies
```

## Output Categories

Verbosity controls **what categories of output** are shown, not just log severity. The system uses semantic output categories defined in `logger/output.go`.

### Category Reference

| Category | Level | Description |
|----------|-------|-------------|
| Results, Errors | 0 | Final results and critical errors (always shown) |
| Progress | 1 | Startup messages, plugin status, progress updates |
| Ax AST | 1 | Parsed query AST for Ax queries |
| Plugin Status | 1 | Plugin load/unload notifications |
| Ax Matches | 2 | What attestations matched the query |
| Timing | 2 | Operation timing (opt-in, auto-shown if slow) |
| Config | 2 | Configuration details |
| Plugin Stdout | 3 | Standard output from plugins |
| Plugin Stderr | 3 | Standard error from plugins |
| SQL Queries | 4 | Raw SQL queries executed |
| gRPC Calls | 4 | gRPC method calls |
| Full Payloads | 4 | Complete request/response bodies |

### Special Behaviors

#### Timing with Slow Threshold

Timing information has special handling via `logger.ShouldShowTiming()`:
- At `-vv` or higher: Always show timing
- At any level: Auto-show if operation is slow (>100ms)

```go
if logger.ShouldShowTiming(verbosity, durationMS) {
    fmt.Printf("Operation took %dms\n", durationMS)
}
```

#### Ax Query Output

Ax queries have granular output control:
- Level 0: Final results only
- Level 1: Parsed AST shown
- Level 2: AST + matched attestations
- Level 4: AST + matches + raw SQL query

## Implementation

### Checking Output Categories

Use `logger.ShouldOutput()` to check if output should be shown:

```go
import "github.com/teranos/QNTX/logger"

func executeQuery(verbosity int, query string) {
    // Level 1+: Show parsed AST
    if logger.ShouldOutput(verbosity, logger.OutputAxAST) {
        fmt.Printf("Parsed AST: %v\n", ast)
    }

    // Level 2+: Show what matched
    if logger.ShouldOutput(verbosity, logger.OutputAxMatches) {
        fmt.Printf("Matched %d attestations\n", len(matches))
    }

    // Level 4+: Show raw SQL
    if logger.ShouldOutput(verbosity, logger.OutputSQLQueries) {
        fmt.Printf("SQL: %s\n", sqlQuery)
    }
}
```

### Logger Level Mapping

Verbosity also maps to zap log levels via `logger.VerbosityToLevel()`:

| Verbosity | Log Level |
|-----------|-----------|
| 0 (none)  | WarnLevel |
| 1 (-v)    | InfoLevel |
| 2+ (-vv)  | DebugLevel |

### Symbol-Aware Logging

Use structured symbol logging instead of embedding symbols in messages:

```go
// Instead of:
logger.Infow(sym.Pulse + " Job started", "job_id", id)

// Use:
logger.PulseInfow("Job started", "job_id", id)

// Or with instance loggers:
pulseLog := logger.AddPulseSymbol(s.logger)
pulseLog.Infow("Job started", "job_id", id)
```

This keeps log messages clean and makes symbols queryable as structured fields.

## Level Definitions

### Level 0 (Default)

Clean, user-facing output only:
- Final results
- Critical errors
- Essential progress messages

Hides parsing details, internal processing, SQL queries, and debug information.

### Level 1 (`-v`)

Progress and status information:
- Startup/shutdown events
- Plugin status changes
- Parsed query AST
- Warnings that don't stop execution

### Level 2 (`-vv`)

Debug information for troubleshooting:
- Matched attestations for Ax queries
- Operation timing (plus auto-show for slow >100ms)
- Configuration details
- Full result sets

### Level 3 (`-vvv`)

Deep debugging context:
- Plugin stdout/stderr logs
- Full error details
- Stack traces
- Internal state inspection

### Level 4 (`-vvvv`)

Ultra-verbose troubleshooting:
- Raw SQL queries
- gRPC method calls and timing
- Full request/response bodies
- All subsystem-specific debugging

## ax Command Examples

### Level 0 (Default)

```
Subject: "user_123"
Predicate: "has_skill"
Object: "Go"

1 attestation found
```

### Level 1 (`-v`)

```
Parsing query...
  Subject: "user_123"
  Predicate: "has_skill"
  Object: "Go"
  Context: (none)

Executing query...
Found 1 matching attestation

Results:
  [attestation details...]
```

### Level 2 (`-vv`)

```
Parsing query...
  Subject: "user_123"
  Predicate: "has_skill"
  Object: "Go"

Matched attestations:
  - ats_123abc... (created 2024-01-15)

Execution time: 2.3ms

Results with full context:
  [Complete attestation data with metadata...]
```

### Level 4 (`-vvvv`)

```
[All Level 2 output plus:]

SQL: SELECT * FROM attestations WHERE subject = ? AND predicate = ? AND object = ?
Parameters: ["user_123", "has_skill", "Go"]
```

## Implementation Notes

The verbosity flag is defined globally in the root command and available to all subcommands. Commands interpret levels based on their specific needs while following the general pattern above.

See `logger/output.go` for the complete list of output categories and their verbosity requirements.
