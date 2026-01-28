package pulse

// ProgressEmitter defines the domain-agnostic interface for emitting progress updates
// during long-running operations. This interface lives in the infrastructure layer (pulse)
// and should NOT contain any domain-specific methods.
//
// Domain packages can:
// 1. Use these methods directly for generic progress reporting
// 2. Create wrapper emitters that add domain-specific convenience methods
//    (see ats/ix/progress_domain_agnostic_test.go for examples)
type ProgressEmitter interface {
	// EmitStage announces the start of a processing stage
	EmitStage(stage string, message string)

	// EmitProgress announces batch progress with count and optional metadata.
	// This is the generic replacement for domain-specific methods like EmitAttestations.
	// Domains can pass entity data as metadata maps.
	EmitProgress(count int, metadata map[string]interface{})

	// EmitComplete announces successful completion with summary
	EmitComplete(summary map[string]interface{})

	// EmitError announces an error during processing
	EmitError(stage string, err error)

	// EmitInfo emits general informational message
	EmitInfo(message string)
}

// ProgressEntity represents a generic entity for progress tracking.
// This is domain-agnostic - any domain can use it for their entities.
type ProgressEntity struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// JobBroadcaster is an optional interface that ProgressEmitter implementations
// can implement to support job tracking and broadcasting to UI clients.
type JobBroadcaster interface {
	// BroadcastJobUpdate sends a job update to all connected clients
	// The job parameter should be of type *async.Job but is interface{} to avoid import cycles
	BroadcastJobUpdate(job interface{})
}

// TaskTracker is an optional interface that ProgressEmitter implementations
// can implement to support fine-grained task tracking (e.g., individual items being processed).
// This enables UI visualizations like progress squares.
type TaskTracker interface {
	// AddTask registers a new task that will be tracked
	// taskID: unique identifier (e.g., item ID, entity ID)
	// taskName: display name (e.g., "Entity-A", "Item-123")
	AddTask(taskID string, taskName string)

	// UpdateTaskStatus updates a task's completion status
	// taskID: the task to update
	// completed: true if task finished successfully, false if failed
	// result: optional result summary (e.g., "Processed: 15 items")
	UpdateTaskStatus(taskID string, completed bool, result string)
}

// LLMStreamBroadcaster is an optional interface for broadcasting LLM streaming events.
type LLMStreamBroadcaster interface {
	// BroadcastLLMStream forwards LLM streaming events to clients.
	BroadcastLLMStream(jobID, taskID, content string, done bool, err error, model, stage string)
}
