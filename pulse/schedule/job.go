// Package schedule provides recurring job scheduling with pulse control.
package schedule

import "time"

// Job represents a recurring scheduled execution job
type Job struct {
	ID              string
	ATSCode         string // Original ATS code (for display/audit)
	HandlerName     string // Async handler to invoke (e.g., "role.jd-ingestion")
	Payload         []byte // Pre-computed JSON payload for the handler
	SourceURL       string // Source URL for deduplication
	IntervalSeconds int
	NextRunAt       *time.Time // Pointer to handle NULL for one-time jobs (forceTriggerJob)
	LastRunAt       *time.Time
	LastExecutionID string
	State           string
	CreatedFromDoc  string
	Metadata        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// State constants for scheduled jobs
const (
	StateActive   = "active"   // Job is running on schedule
	StatePaused   = "paused"   // Job is temporarily paused by user
	StateStopping = "stopping" // Job is in process of stopping
	StateInactive = "inactive" // Job is inactive (not running, not scheduled)
	StateDeleted  = "deleted"  // Job has been deleted by user (soft delete)
)
