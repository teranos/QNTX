// Package schedule provides recurring job scheduling with pulse control.
package schedule

import "github.com/teranos/QNTX/plugin/grpc/protocol"

// Job is protocol.ScheduledJob (ADR-028): the read view a caller is handed,
// which is the declaration plus what the ticks derive. Timestamps are RFC3339.
type Job = protocol.ScheduledJob

// State constants for scheduled jobs
const (
	StateActive   = "active"   // Job is running on schedule
	StatePaused   = "paused"   // Job is temporarily paused by user
	StateStopping = "stopping" // Job is in process of stopping
	StateInactive = "inactive" // Job is inactive (not running, not scheduled)
	StateDeleted  = "deleted"  // Job has been deleted by user (soft delete)
)
