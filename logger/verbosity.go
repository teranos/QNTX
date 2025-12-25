package logger

import "go.uber.org/zap/zapcore"

// Verbosity level constants for CLI flag counts
const (
	VerbosityUser  = 0 // No flags: user-facing output only
	VerbosityInfo  = 1 // -v: informational messages
	VerbosityDebug = 2 // -vv: debug messages
	VerbosityTrace = 3 // -vvv: trace-level debugging
	VerbosityAll   = 4 // -vvvv: dump full data structures
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
