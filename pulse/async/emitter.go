package async

import (
	"time"

	"github.com/teranos/QNTX/ats/ix"
	"go.uber.org/zap"
)

// JobProgressEmitter implements ix.ProgressEmitter for async job progress updates.
// ✿/❀ Opening/Closing: Now saves checkpoints on stage transitions.
type JobProgressEmitter struct {
	job               *Job
	queue             *Queue
	streamBroadcaster interface{}        // Optional: WebSocket broadcaster for LLM streaming (nil for CLI jobs)
	log               *zap.SugaredLogger // Context-aware logger with job_id pre-configured
}

// NewJobProgressEmitter creates a new progress emitter for an async job.
// The provided logger should be the WorkerPool's logger for proper WebSocket broadcasting.
func NewJobProgressEmitter(job *Job, queue *Queue, streamBroadcaster interface{}, baseLogger *zap.SugaredLogger) *JobProgressEmitter {
	// Create context-aware logger with job_id pre-configured
	// Use provided logger (from WorkerPool) instead of global logger
	contextLogger := baseLogger.With("job_id", job.ID)

	return &JobProgressEmitter{
		job:               job,
		queue:             queue,
		streamBroadcaster: streamBroadcaster,
		log:               contextLogger,
	}
}

// EmitStage updates progress on stage transition.
// Note: Checkpointing is now handled by handlers via payload updates.
func (e *JobProgressEmitter) EmitStage(stage, message string) {
	// Update job in database to save progress
	if err := e.queue.UpdateJob(e.job); err != nil {
		e.log.Warnw("Failed to update job for stage",
			"stage", stage,
			"error", err,
		)
	}
}

// EmitAttestations updates job progress for attestation creation.
func (e *JobProgressEmitter) EmitAttestations(count int, entities []ix.AttestationEntity) {
	// Update job progress
	e.job.UpdateProgress(e.job.Progress.Current + count)

	// Update job in database
	if err := e.queue.UpdateJob(e.job); err != nil {
		e.log.Warnw("Failed to update job progress",
			"count", count,
			"error", err,
		)
	}
}

// EmitCandidateMatch updates job progress for candidate scoring.
func (e *JobProgressEmitter) EmitCandidateMatch(candidateID string, score float64, qualified bool, reasoning string) {
	// Update job progress
	e.job.UpdateProgress(e.job.Progress.Current + 1)

	// Record cost (assuming cost per score from config)
	// TODO: Get actual cost from config
	e.job.RecordCost(0.002)

	// Update job in database
	if err := e.queue.UpdateJob(e.job); err != nil {
		e.log.Warnw("Failed to update job progress",
			"candidate_id", candidateID,
			"error", err,
		)
	}
}

// EmitComplete handles job completion (handled by worker).
func (e *JobProgressEmitter) EmitComplete(summary map[string]interface{}) {
	// Job completion handled by worker
}

// EmitError logs errors, updates job state, and broadcasts to WebSocket clients.
func (e *JobProgressEmitter) EmitError(stage string, err error) {
	// Classify the error for structured reporting
	ctx := ClassifyError(stage, err)

	// Log error with classification
	e.log.Errorw("Job error",
		"stage", stage,
		"error_code", ctx.Code,
		"error", err,
		"retryable", ctx.Retryable,
		"recoverable", ctx.Recoverable,
	)

	// Update job error state in database
	e.job.Error = ctx.Message
	if err := e.queue.UpdateJob(e.job); err != nil {
		e.log.Warnw("Failed to update job error state",
			"error", err,
		)
	}

	// Broadcast error event to WebSocket clients if broadcaster is available
	if e.streamBroadcaster == nil {
		return // No broadcaster - CLI job or standalone execution
	}

	// Type-check broadcaster for the broadcastIxProgress method
	// Define the event structure inline to match internal/server/ix.go:IxProgressEvent
	type ixProgressEvent struct {
		Type      string                 `json:"type"`
		Timestamp time.Time              `json:"timestamp"`
		Data      map[string]interface{} `json:"data"`
	}

	type serverBroadcaster interface {
		broadcastIxProgress(event ixProgressEvent)
	}

	// Try to cast to the server type that has broadcastIxProgress
	if srv, ok := e.streamBroadcaster.(serverBroadcaster); ok {
		event := ixProgressEvent{
			Type:      "error",
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"job_id":      e.job.ID,
				"stage":       ctx.Stage,
				"code":        string(ctx.Code),
				"error":       ctx.Message,
				"retryable":   ctx.Retryable,
				"recoverable": ctx.Recoverable,
			},
		}
		srv.broadcastIxProgress(event)
	}
}

// EmitInfo logs informational messages.
func (e *JobProgressEmitter) EmitInfo(message string) {
	e.log.Info(message)
}

// BroadcastLLMStream forwards LLM streaming events to WebSocket clients (if broadcaster is set).
// This restores real-time token-by-token display in the UI for async jobs.
func (e *JobProgressEmitter) BroadcastLLMStream(jobID, taskID, content string, done bool, err error, model, stage string) {
	if e.streamBroadcaster == nil {
		return // No broadcaster - CLI job or standalone execution
	}

	// Type-check broadcaster for the expected interface
	type llmStreamBroadcaster interface {
		BroadcastLLMStream(jobID, taskID, content string, done bool, err error, model, stage string)
	}

	if broadcaster, ok := e.streamBroadcaster.(llmStreamBroadcaster); ok {
		// Use job ID from our tracked job if not provided
		if jobID == "" && e.job != nil {
			jobID = e.job.ID
		}
		broadcaster.BroadcastLLMStream(jobID, taskID, content, done, err, model, stage)
	}
}
