package ix

// LogStore defines the interface for persisting task logs
type LogStore interface {
	WriteLog(jobID, stage, level, message string, metadata map[string]interface{}) error
}

// LogCapturingEmitter wraps a ProgressEmitter and captures all emissions to a LogStore.
// This enables persistent logging of job execution for debugging and observability.
type LogCapturingEmitter struct {
	underlying ProgressEmitter
	logStore   LogStore
	jobID      string
	stage      string // Current stage for context
}

// NewLogCapturingEmitter creates a new log-capturing emitter wrapper
func NewLogCapturingEmitter(underlying ProgressEmitter, logStore LogStore, jobID string) *LogCapturingEmitter {
	return &LogCapturingEmitter{
		underlying: underlying,
		logStore:   logStore,
		jobID:      jobID,
	}
}

// EmitStage logs the stage transition and passes through to underlying emitter
func (e *LogCapturingEmitter) EmitStage(stage string, message string) {
	e.stage = stage
	e.writeLog("info", message)
	e.underlying.EmitStage(stage, message)
}

// EmitAttestations logs attestation count and passes through
func (e *LogCapturingEmitter) EmitAttestations(count int, entities []AttestationEntity) {
	e.writeLog("info", "Created attestations", map[string]interface{}{
		"count": count,
	})
	e.underlying.EmitAttestations(count, entities)
}

// EmitComplete logs completion and passes through
func (e *LogCapturingEmitter) EmitComplete(summary map[string]interface{}) {
	e.writeLog("info", "Job completed", summary)
	e.underlying.EmitComplete(summary)
}

// EmitError logs the error and passes through
func (e *LogCapturingEmitter) EmitError(stage string, err error) {
	e.stage = stage
	e.writeLog("error", err.Error())
	e.underlying.EmitError(stage, err)
}

// EmitInfo logs the info message and passes through
func (e *LogCapturingEmitter) EmitInfo(message string) {
	e.writeLog("info", message)
	e.underlying.EmitInfo(message)
}

// writeLog persists a log entry, warning via underlying emitter if write fails
func (e *LogCapturingEmitter) writeLog(level, message string, metadata ...map[string]interface{}) {
	var meta map[string]interface{}
	if len(metadata) > 0 {
		meta = metadata[0]
	}

	if err := e.logStore.WriteLog(e.jobID, e.stage, level, message, meta); err != nil {
		// Warn via underlying emitter that log persistence failed
		e.underlying.EmitInfo("Failed to persist log to database: " + err.Error())
	}
}
