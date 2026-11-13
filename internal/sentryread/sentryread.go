// Package sentryread is what the node asks Sentry about what it sent it (ADR-042), and what comes back.
//
// "can you do it please to the degree that is feasible given sentry access?"
//
// "ADR-025 is DEAD ok?"
//
// "And the node gives Agents access to the MCP of Sentry"
//
// Three questions and no more: a table of rows, a series of buckets, and what
// became of what was sent. Who answers them is the caller's: the node asks
// them of the Sentry MCP server it consumes (server/report_sentry.go), and holds
// no token of its own for Sentry.
package sentryread

import "time"

// The datasets a question is asked of, as the Sentry API names them.
const (
	DatasetLogs    = "ourlogs"
	DatasetSpans   = "spans"
	DatasetMetrics = "tracemetrics"
)

// Window is the time a question covers.
type Window struct {
	Start time.Time
	End   time.Time
}

// TableQuery asks for rows: fields, often aggregates, grouped by the plain
// fields among them.
type TableQuery struct {
	Dataset string
	Fields  []string
	Query   string
	Sort    string
	Limit   int
	Window  Window
}

// SeriesQuery asks for one aggregate per bucket across a window, the newest
// bucket ending where the window does.
type SeriesQuery struct {
	Dataset string
	YAxis   string
	Query   string
	Bucket  time.Duration
	Window  Window
}

// Point is one bucket of a series.
type Point struct {
	At    time.Time
	Value float64
}

// Outcome is how many events of one category Sentry took, or dropped for one
// reason, as Sentry names both: accepted, or rate_limited for
// error_usage_exceeded.
type Outcome struct {
	Outcome string
	Reason  string
	Count   float64
}

// CategoryError is the category of error events, as Sentry names it.
const CategoryError = "error"
