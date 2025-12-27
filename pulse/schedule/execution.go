package schedule

// Execution represents a single execution of a scheduled Pulse job
//
// Each time a scheduled job runs, an Execution record is created to track:
// - Timing (started_at, completed_at, duration)
// - Status (running, completed, failed)
// - Output (logs, result summary, errors)
// - Link to async job for detailed processing
//
// This provides execution history for debugging, monitoring, performance
// tracking, and failure troubleshooting.
type Execution struct {
	// Identity
	ID             string  `json:"id"`                     // PEX_{random}_{timestamp} format
	ScheduledJobID string  `json:"scheduled_job_id"`       // FK to ScheduledJob
	AsyncJobID     *string `json:"async_job_id,omitempty"` // Optional FK to async job

	// Execution status
	Status string `json:"status"` // "running", "completed", "failed"

	// Timing
	StartedAt   string  `json:"started_at"`             // RFC3339 timestamp
	CompletedAt *string `json:"completed_at,omitempty"` // RFC3339 timestamp (null if running)
	DurationMs  *int    `json:"duration_ms,omitempty"`  // Milliseconds (null if running)

	// Output capture
	Logs          *string `json:"logs,omitempty"`           // Captured stdout/stderr
	ResultSummary *string `json:"result_summary,omitempty"` // Brief summary
	ErrorMessage  *string `json:"error_message,omitempty"`  // Error if failed

	// Metadata
	CreatedAt string `json:"created_at"` // RFC3339 timestamp
	UpdatedAt string `json:"updated_at"` // RFC3339 timestamp
}

// Execution status constants for type safety
const (
	ExecutionStatusRunning   = "running"
	ExecutionStatusCompleted = "completed"
	ExecutionStatusFailed    = "failed"
)
