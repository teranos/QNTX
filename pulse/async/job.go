// Package async provides asynchronous IX job processing with pulse control.
package async

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/vanity-id"
)

// JobStatus represents the current state of a job
type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusPaused    JobStatus = "paused"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// IsValidStatus returns true if the status string is a valid JobStatus
func IsValidStatus(s string) bool {
	switch JobStatus(s) {
	case JobStatusQueued, JobStatusRunning, JobStatusPaused,
		JobStatusCompleted, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

// PulseState represents the pulse rate limiting and budget state for a job
type PulseState struct {
	CallsThisMinute int     `json:"calls_this_minute,omitempty"`
	CallsRemaining  int     `json:"calls_remaining,omitempty"`
	SpendToday      float64 `json:"spend_today,omitempty"`
	SpendThisMonth  float64 `json:"spend_this_month,omitempty"`
	BudgetRemaining float64 `json:"budget_remaining,omitempty"`
	IsPaused        bool    `json:"is_paused,omitempty"`
	PauseReason     string  `json:"pause_reason,omitempty"` // "budget_exceeded", "rate_limit", "user_requested"
}

// Progress represents job progress information
type Progress struct {
	Current int `json:"current,omitempty"` // Completed operations
	Total   int `json:"total,omitempty"`   // Total operations
}

// Percentage calculates progress as a percentage (0-100)
func (p Progress) Percentage() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Current) / float64(p.Total) * 100
}

// Job represents an async IX operation
//
// ARCHITECTURE: Generic job system with handler-based execution
// - Infrastructure (pulse/async) is domain-agnostic
// - Domain packages provide handlers and payloads
// - HandlerName identifies which handler executes the job
// - Payload contains handler-specific data (domain logic controls structure)
type Job struct {
	ID           string          `json:"id"`
	HandlerName  string          `json:"handler_name"`      // "data.batch-import", "bio.sequence-align"
	Payload      json.RawMessage `json:"payload,omitempty"` // Handler-specific data (domain-owned)
	Source       string          `json:"source"`            // For deduplication and logging
	Status       JobStatus       `json:"status"`
	Progress     Progress        `json:"progress,omitempty"`
	CostEstimate float64         `json:"cost_estimate,omitempty"`
	CostActual   float64         `json:"cost_actual,omitempty"`
	PulseState   *PulseState     `json:"pulse_state,omitempty"`
	Error        string          `json:"error,omitempty"`
	ParentJobID  string          `json:"parent_job_id,omitempty"` // For tasks grouped under parent job
	RetryCount   int             `json:"retry_count,omitempty"`   // Number of retry attempts (max 2)
	CreatedAt    time.Time       `json:"created_at"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// NewJobWithPayload creates a new generic job with handler name and typed payload.
//
// Example:
//
//	payload := BatchImportPayload{SourceURL: "https://...", RecordIDs: []string{"1", "2"}}
//	payloadJSON, _ := json.Marshal(payload)
//	job, _ := async.NewJobWithPayload("data.batch-import", "https://...", payloadJSON, 100, 0.50, "user@example.com")
func NewJobWithPayload(handlerName string, source string, payload json.RawMessage, totalOps int, estimatedCost float64, actor string) (*Job, error) {
	return NewChildJobWithPayload(handlerName, source, payload, totalOps, estimatedCost, actor, "")
}

// NewChildJobWithPayload creates a new job with an optional parent job ID.
// Use this when creating child jobs that should be grouped under a parent orchestrator job.
func NewChildJobWithPayload(handlerName string, source string, payload json.RawMessage, totalOps int, estimatedCost float64, actor string, parentJobID string) (*Job, error) {
	if handlerName == "" {
		return nil, errors.New("handlerName cannot be empty")
	}
	if actor == "" {
		actor = "system"
	}

	// Generate unique job ASID
	// Format: JB + random(2) + handler(5) + random(2) + process(7) + random(2) + source(5) + random(4) + actor(3)
	jobID, err := id.GenerateJobASID(handlerName, source, actor)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate job ASID")
	}

	now := time.Now()
	return &Job{
		ID:           jobID,
		HandlerName:  handlerName,
		Payload:      payload,
		Source:       source,
		Status:       JobStatusQueued,
		Progress:     Progress{Current: 0, Total: totalOps},
		CostEstimate: estimatedCost,
		CostActual:   0.0,
		ParentJobID:  parentJobID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// Start marks the job as running
func (j *Job) Start() {
	now := time.Now()
	j.Status = JobStatusRunning
	j.StartedAt = &now
	j.UpdatedAt = now
}

// Pause marks the job as paused
func (j *Job) Pause(reason string) {
	j.Status = JobStatusPaused
	j.UpdatedAt = time.Now()
	if j.PulseState != nil {
		j.PulseState.IsPaused = true
		j.PulseState.PauseReason = reason
	}
}

// Resume marks the job as running again
func (j *Job) Resume() {
	j.Status = JobStatusRunning
	j.UpdatedAt = time.Now()
	if j.PulseState != nil {
		j.PulseState.IsPaused = false
		j.PulseState.PauseReason = ""
	}
}

// Complete marks the job as completed
func (j *Job) Complete() {
	now := time.Now()
	j.Status = JobStatusCompleted
	j.CompletedAt = &now
	j.UpdatedAt = now
}

// Fail marks the job as failed with an error message
func (j *Job) Fail(err error) {
	now := time.Now()
	j.Status = JobStatusFailed
	j.Error = err.Error()
	j.CompletedAt = &now
	j.UpdatedAt = now
}

// Cancel marks the job as cancelled with a reason
func (j *Job) Cancel(reason string) {
	now := time.Now()
	j.Status = JobStatusCancelled
	j.Error = reason
	j.CompletedAt = &now
	j.UpdatedAt = now
}

// UpdateProgress updates the job's progress
func (j *Job) UpdateProgress(current int) {
	j.Progress.Current = current
	j.UpdatedAt = time.Now()
}

// RecordCost adds to the actual cost incurred
func (j *Job) RecordCost(cost float64) {
	j.CostActual += cost
	j.UpdatedAt = time.Now()
}

// UpdatePulseState updates the pulse state
func (j *Job) UpdatePulseState(state *PulseState) {
	j.PulseState = state
	j.UpdatedAt = time.Now()
}

// MarshalPulseState converts PulseState to JSON string
func MarshalPulseState(state *PulseState) (string, error) {
	if state == nil {
		return "", nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal pulse state")
	}
	return string(data), nil
}

// UnmarshalPulseState converts JSON string to PulseState
func UnmarshalPulseState(data string) (*PulseState, error) {
	if data == "" {
		return nil, nil
	}
	var state PulseState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal pulse state")
	}
	return &state, nil
}
