// Package schedule provides recurring job scheduling with pulse control.
package schedule

import "github.com/teranos/QNTX/plugin/grpc/protocol"

// Job is protocol.ScheduledJob (ADR-028): the read view a caller is handed,
// which is the declaration plus what the ticks derive. Timestamps are RFC3339.
type Job = protocol.ScheduledJob

// The set lives in schedule.proto. These read it rather than restate it, so a
// state added there cannot be missing here.
var (
	StateActive   = protocol.ScheduleState_active.String()
	StatePaused   = protocol.ScheduleState_paused.String()
	StateStopping = protocol.ScheduleState_stopping.String()
	StateInactive = protocol.ScheduleState_inactive.String()
	StateDeleted  = protocol.ScheduleState_deleted.String()
)
