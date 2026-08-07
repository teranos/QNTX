package schedule

import "github.com/teranos/QNTX/plugin/grpc/protocol"

// Execution is protocol.Execution (ADR-006): one run of a scheduled job, with
// its timing, status, captured output and link to the async job. Aliased
// rather than renamed at every call site, so the shape has one definition.
type Execution = protocol.Execution

// The set lives in schedule.proto, same as ScheduleState.
var (
	ExecutionStatusRunning   = protocol.ExecutionStatus_running.String()
	ExecutionStatusCompleted = protocol.ExecutionStatus_completed.String()
	ExecutionStatusFailed    = protocol.ExecutionStatus_failed.String()
)
