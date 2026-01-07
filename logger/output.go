package logger

// Output controls what categories of information are shown at each verbosity level.
//
// Unlike log levels (which filter by severity), output categories control
// WHAT types of information are displayed regardless of severity.
//
// Verbosity Levels:
//
//	0 (default) - User-facing output only: results, errors with hints, final status
//	1 (-v)      - + Progress, startup info, plugin status, operation summaries
//	2 (-vv)     - + Ax query details, timing, config loaded, HTTP requests
//	3 (-vvv)    - + Plugin stdout/stderr, gRPC calls, SQL queries, internal flow
//	4 (-vvvv)   - + Full request/response bodies, data structure dumps

// OutputCategory defines a category of output that can be enabled/disabled
type OutputCategory int

const (
	// Level 0 (default) - Always shown
	OutputResults    OutputCategory = iota // Query results, command output
	OutputErrors                           // Errors with hints and resolution steps
	OutputUserStatus                       // Final success/failure status

	// Level 1 (-v) - Informational
	OutputProgress      // Progress indicators (e.g., "Processing 50/100 commits")
	OutputStartup       // Startup banners, config summary
	OutputPluginStatus  // Plugin loaded/unloaded/health status
	OutputOperationInfo // High-level operation summaries

	// Level 2 (-vv) - Detailed
	OutputAxQueries  // Ax query parsing and execution details
	OutputTiming     // Operation timing (e.g., "query took 42ms")
	OutputConfig     // Config values loaded/applied
	OutputHTTPCalls  // External HTTP requests made
	OutputDBStats    // Database statistics and connection info

	// Level 3 (-vvv) - Debug
	OutputPluginLogs // Plugin stdout/stderr forwarding
	OutputGRPCCalls  // gRPC request/response summaries
	OutputSQLQueries // Individual SQL queries executed
	OutputInternalOp // Internal operation flow (function entry/exit)

	// Level 4 (-vvvv) - Full dump
	OutputRequestBody  // Full HTTP/gRPC request bodies
	OutputResponseBody // Full HTTP/gRPC response bodies
	OutputDataDump     // Full data structure contents
)

// categoryLevels maps each output category to its minimum verbosity level
var categoryLevels = map[OutputCategory]int{
	// Level 0 - Always shown
	OutputResults:    VerbosityUser,
	OutputErrors:     VerbosityUser,
	OutputUserStatus: VerbosityUser,

	// Level 1 - Informational
	OutputProgress:      VerbosityInfo,
	OutputStartup:       VerbosityInfo,
	OutputPluginStatus:  VerbosityInfo,
	OutputOperationInfo: VerbosityInfo,

	// Level 2 - Detailed
	OutputAxQueries: VerbosityDebug,
	OutputTiming:    VerbosityDebug,
	OutputConfig:    VerbosityDebug,
	OutputHTTPCalls: VerbosityDebug,
	OutputDBStats:   VerbosityDebug,

	// Level 3 - Debug
	OutputPluginLogs: VerbosityTrace,
	OutputGRPCCalls:  VerbosityTrace,
	OutputSQLQueries: VerbosityTrace,
	OutputInternalOp: VerbosityTrace,

	// Level 4 - Full dump
	OutputRequestBody:  VerbosityAll,
	OutputResponseBody: VerbosityAll,
	OutputDataDump:     VerbosityAll,
}

// ShouldOutput returns true if the given category should be shown at the given verbosity
func ShouldOutput(verbosity int, category OutputCategory) bool {
	minLevel, ok := categoryLevels[category]
	if !ok {
		// Unknown category, default to highest verbosity required
		return verbosity >= VerbosityAll
	}
	return verbosity >= minLevel
}

// categoryNames provides human-readable names for output categories
var categoryNames = map[OutputCategory]string{
	OutputResults:      "results",
	OutputErrors:       "errors",
	OutputUserStatus:   "status",
	OutputProgress:     "progress",
	OutputStartup:      "startup",
	OutputPluginStatus: "plugin-status",
	OutputOperationInfo: "operation-info",
	OutputAxQueries:    "ax-queries",
	OutputTiming:       "timing",
	OutputConfig:       "config",
	OutputHTTPCalls:    "http",
	OutputDBStats:      "db-stats",
	OutputPluginLogs:   "plugin-logs",
	OutputGRPCCalls:    "grpc",
	OutputSQLQueries:   "sql",
	OutputInternalOp:   "internal",
	OutputRequestBody:  "request-body",
	OutputResponseBody: "response-body",
	OutputDataDump:     "data-dump",
}

// CategoryName returns the human-readable name for an output category
func CategoryName(category OutputCategory) string {
	if name, ok := categoryNames[category]; ok {
		return name
	}
	return "unknown"
}

// EnabledCategories returns all output categories enabled at the given verbosity
func EnabledCategories(verbosity int) []OutputCategory {
	var enabled []OutputCategory
	for cat, minLevel := range categoryLevels {
		if verbosity >= minLevel {
			enabled = append(enabled, cat)
		}
	}
	return enabled
}

// VerbosityDescription returns a description of what's shown at each level
func VerbosityDescription(verbosity int) string {
	switch verbosity {
	case VerbosityUser:
		return "results and errors only"
	case VerbosityInfo:
		return "results, errors, progress, and status"
	case VerbosityDebug:
		return "above + queries, timing, config details"
	case VerbosityTrace:
		return "above + plugin logs, SQL, gRPC calls"
	case VerbosityAll:
		return "full output including request/response bodies"
	default:
		if verbosity > VerbosityAll {
			return "maximum verbosity"
		}
		return "unknown verbosity level"
	}
}
