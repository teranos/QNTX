package logger

import "go.uber.org/zap/zapcore"

// Verbosity level constants for CLI flag counts.
//
// These levels control WHAT categories of output are shown, not just log severity.
// See output.go for the full category system.
//
// Example usage:
//
//	if logger.ShouldOutput(verbosity, logger.OutputPluginLogs) {
//	    fmt.Printf("[plugin] %s\n", line)
//	}
//
//	if logger.ShouldOutput(verbosity, logger.OutputAxQueries) {
//	    fmt.Printf("Executing query: %s\n", query)
//	}
const (
	VerbosityUser  = 0 // No flags: results and errors only
	VerbosityInfo  = 1 // -v: + progress, startup, plugin status
	VerbosityDebug = 2 // -vv: + queries, timing, config details
	VerbosityTrace = 3 // -vvv: + plugin logs, SQL, gRPC calls
	VerbosityAll   = 4 // -vvvv: + full request/response bodies
)

// VerbosityToLevel maps verbosity flags (-v, -vv, etc.) to zap log levels
//
// Mapping:
//
//	0 (none)  -> WarnLevel  (errors and warnings only)
//	1 (-v)    -> InfoLevel  (+ informational messages)
//	2 (-vv)   -> DebugLevel (+ debug messages)
//	3+ (-vvv) -> DebugLevel (zap doesn't have finer levels, but we track for custom behavior)
func VerbosityToLevel(verbosity int) zapcore.Level {
	switch verbosity {
	case VerbosityUser:
		return zapcore.WarnLevel
	case VerbosityInfo:
		return zapcore.InfoLevel
	case VerbosityDebug:
		return zapcore.DebugLevel
	case VerbosityTrace:
		return zapcore.DebugLevel
	case VerbosityAll:
		return zapcore.DebugLevel
	default:
		// For any verbosity > VerbosityAll, use DebugLevel
		return zapcore.DebugLevel
	}
}

// ShouldLogTrace returns true for verbosity >= 3 (-vvv)
// Use this for very detailed trace logging
func ShouldLogTrace(verbosity int) bool {
	return verbosity >= VerbosityTrace
}

// ShouldLogAll returns true for verbosity >= 4 (-vvvv)
// Use this for dumping full data structures
func ShouldLogAll(verbosity int) bool {
	return verbosity >= VerbosityAll
}

// LevelName returns a human-readable name for verbosity level
func LevelName(verbosity int) string {
	switch verbosity {
	case VerbosityUser:
		return "User"
	case VerbosityInfo:
		return "Info (-v)"
	case VerbosityDebug:
		return "Debug (-vv)"
	case VerbosityTrace:
		return "Trace (-vvv)"
	case VerbosityAll:
		return "All (-vvvv)"
	default:
		if verbosity > VerbosityAll {
			return "All (-vvvv+)"
		}
		return "Unknown"
	}
}
