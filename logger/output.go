package logger

// Output controls what categories of information are shown at each verbosity level.
//
// Unlike log levels (which filter by severity), output categories control
// WHAT types of information are displayed regardless of severity.
//
// Verbosity Levels:
//
//	0 (default) - User-facing output only: results, errors with hints
//	1 (-v)      - + Progress, startup info, Ax AST, plugin status
//	2 (-vv)     - + Ax matches, timing, config loaded, HTTP requests
//	3 (-vvv)    - + Plugin stdout/stderr, gRPC calls, internal flow
//	4 (-vvvv)   - + SQL queries, full request/response bodies, data dumps

// OutputCategory defines a category of output that can be enabled/disabled
type OutputCategory int

const (
	// Level 0 (default) - Always shown
	OutputResults    OutputCategory = iota // Query results, command output
	OutputErrors                           // Errors with hints and resolution steps
	OutputUserStatus                       // Final success/failure status

	// Level 1 (-v) - Informational
	OutputProgress       // Progress indicators (e.g., "Processing 50/100 commits")
	OutputStartup        // Startup banners, config summary
	OutputPluginStatus   // Plugin loaded/unloaded/health status
	OutputOperationInfo  // High-level operation summaries
	OutputAxAST          // Ax query parsed AST

	// Level 2 (-vv) - Detailed
	OutputAxMatches    // What matched in Ax query (predicates, subjects)
	OutputTiming       // Operation timing (e.g., "query took 42ms")
	OutputConfig       // Config values loaded/applied
	OutputHTTPRequests // Outgoing HTTP request URLs and methods
	OutputHTTPStatus   // HTTP response status codes
	OutputDBStats      // Database statistics and connection info
	OutputPluginConfig // Plugin configuration being applied

	// Level 3 (-vvv) - Debug
	OutputPluginStdout // Plugin process stdout
	OutputPluginStderr // Plugin process stderr
	OutputGRPCMethod   // gRPC method calls (method name, timing)
	OutputGRPCStatus   // gRPC response status
	OutputInternalFlow // Internal operation flow (function entry/exit)
	OutputAxExecution  // Ax query execution steps

	// Level 4 (-vvvv) - Full dump
	OutputSQLQueries   // Full SQL queries executed
	OutputSQLResults   // SQL query result summaries
	OutputHTTPBody     // Full HTTP request/response bodies
	OutputGRPCBody     // Full gRPC request/response bodies
	OutputDataDump     // Full data structure contents
	OutputAxPlan       // Full Ax query execution plan
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
	OutputAxAST:         VerbosityInfo,

	// Level 2 - Detailed
	OutputAxMatches:    VerbosityDebug,
	OutputTiming:       VerbosityDebug,
	OutputConfig:       VerbosityDebug,
	OutputHTTPRequests: VerbosityDebug,
	OutputHTTPStatus:   VerbosityDebug,
	OutputDBStats:      VerbosityDebug,
	OutputPluginConfig: VerbosityDebug,

	// Level 3 - Debug
	OutputPluginStdout: VerbosityTrace,
	OutputPluginStderr: VerbosityTrace,
	OutputGRPCMethod:   VerbosityTrace,
	OutputGRPCStatus:   VerbosityTrace,
	OutputInternalFlow: VerbosityTrace,
	OutputAxExecution:  VerbosityTrace,

	// Level 4 - Full dump
	OutputSQLQueries: VerbosityAll,
	OutputSQLResults: VerbosityAll,
	OutputHTTPBody:   VerbosityAll,
	OutputGRPCBody:   VerbosityAll,
	OutputDataDump:   VerbosityAll,
	OutputAxPlan:     VerbosityAll,
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
	OutputAxAST:        "ax-ast",
	OutputAxMatches:    "ax-matches",
	OutputTiming:       "timing",
	OutputConfig:       "config",
	OutputHTTPRequests: "http-requests",
	OutputHTTPStatus:   "http-status",
	OutputDBStats:      "db-stats",
	OutputPluginConfig: "plugin-config",
	OutputPluginStdout: "plugin-stdout",
	OutputPluginStderr: "plugin-stderr",
	OutputGRPCMethod:   "grpc-method",
	OutputGRPCStatus:   "grpc-status",
	OutputInternalFlow: "internal-flow",
	OutputAxExecution:  "ax-execution",
	OutputSQLQueries:   "sql-queries",
	OutputSQLResults:   "sql-results",
	OutputHTTPBody:     "http-body",
	OutputGRPCBody:     "grpc-body",
	OutputDataDump:     "data-dump",
	OutputAxPlan:       "ax-plan",
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
		return "results, errors, progress, Ax AST"
	case VerbosityDebug:
		return "above + Ax matches, timing, config"
	case VerbosityTrace:
		return "above + plugin logs, gRPC calls"
	case VerbosityAll:
		return "above + SQL queries, full bodies"
	default:
		if verbosity > VerbosityAll {
			return "maximum verbosity"
		}
		return "unknown verbosity level"
	}
}

// Ax query output helpers

// ShouldShowAxAST returns true if Ax AST should be displayed
func ShouldShowAxAST(verbosity int) bool {
	return ShouldOutput(verbosity, OutputAxAST)
}

// ShouldShowAxMatches returns true if Ax match details should be displayed
func ShouldShowAxMatches(verbosity int) bool {
	return ShouldOutput(verbosity, OutputAxMatches)
}

// ShouldShowAxSQL returns true if Ax SQL queries should be displayed
func ShouldShowAxSQL(verbosity int) bool {
	return ShouldOutput(verbosity, OutputSQLQueries)
}

// Plugin output helpers

// ShouldShowPluginStdout returns true if plugin stdout should be forwarded
func ShouldShowPluginStdout(verbosity int) bool {
	return ShouldOutput(verbosity, OutputPluginStdout)
}

// ShouldShowPluginStderr returns true if plugin stderr should be forwarded
func ShouldShowPluginStderr(verbosity int) bool {
	return ShouldOutput(verbosity, OutputPluginStderr)
}

// Timing helpers

// SlowThresholdMS is the threshold in milliseconds above which timing is always shown
const SlowThresholdMS = 100

// ShouldShowTiming returns true if timing info should be displayed.
// Shows if: verbosity >= 2 (-vv) OR operation exceeded slow threshold.
func ShouldShowTiming(verbosity int, durationMS int64) bool {
	if durationMS >= SlowThresholdMS {
		return true // Always show slow operations
	}
	return ShouldOutput(verbosity, OutputTiming)
}

// ShouldShowTimingAlways returns true if timing should always be shown (slow operation)
func ShouldShowTimingAlways(durationMS int64) bool {
	return durationMS >= SlowThresholdMS
}
