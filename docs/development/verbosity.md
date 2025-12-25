# Verbosity Levels

Progressive verbosity pattern for QNTX CLI commands using the `-v` flag.

## Pattern

```bash
qntx <command>        # Level 0 (default) - Clean output
qntx <command> -v     # Level 1 - Parse details and progress
qntx <command> -vv    # Level 2 - Debug information
qntx <command> -vvv   # Level 3 - Deep debugging context
qntx <command> -vvvv  # Level 4 - Ultra-verbose troubleshooting
```

## Level Definitions

### Level 0 (Default)

Clean, user-facing output only:
- Final results
- Critical errors
- Essential progress messages

Hides parsing details, internal processing, SQL queries, and debug information.

### Level 1 (`-v`)

Parse details and basic progress:
- Parsing breakdowns (subjects, predicates, objects)
- Execution context
- Warnings that don't stop execution
- Query construction details

### Level 2 (`-vv`)

Debug information for troubleshooting:
- SQL queries executed
- Database operations
- Detailed execution timing
- Full result sets
- File paths and configuration

### Level 3 (`-vvv`)

Deep debugging context:
- Full error details
- Stack traces
- Command usage information
- Internal state inspection

### Level 4 (`-vvvv`)

Ultra-verbose troubleshooting:
- All Level 3 output
- Subsystem-specific deep debugging
- Internal data structure dumps

## Integration with Logger

Map verbosity to log levels via `logger.VerbosityToLevel()`:

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

SQL: SELECT * FROM attestations WHERE subject = ? AND predicate = ? AND object = ?
Parameters: ["user_123", "has_skill", "Go"]
Execution time: 2.3ms

Results with full context:
  [Complete attestation data with metadata...]
```

## Implementation Notes

The verbosity flag is defined globally in the root command and available to all subcommands. Commands interpret levels based on their specific needs while following the general pattern above.
