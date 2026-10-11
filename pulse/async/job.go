// Package async provides asynchronous IX job processing with pulse control.
package async

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/errors"
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

// PulseState represents the pulse rate limiting state for a job
type PulseState struct {
	CallsThisMinute int    `json:"calls_this_minute,omitempty"`
	CallsRemaining  int    `json:"calls_remaining,omitempty"`
	IsPaused        bool   `json:"is_paused,omitempty"`
	PauseReason     string `json:"pause_reason,omitempty"` // "rate_limited", "user_requested"
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
// - This type will eventually migrate to proto generation
type Job struct {
	ID            string          `json:"id"`
	HandlerName   string          `json:"handler_name"`      // "data.batch-import", "bio.sequence-align"
	Payload       json.RawMessage `json:"payload,omitempty"` // Handler-specific data (domain-owned)
	Source        string          `json:"source"`            // For deduplication and logging
	Status        JobStatus       `json:"status"`
	Progress      Progress        `json:"progress,omitempty"`
	Pulse         Pulse           `json:"-"` // The pulse state it carries, or that it carries none; JSON says it as pulse_state
	Error         string          `json:"error,omitempty"`
	ErrorDetails  []string        `json:"error_details,omitempty"`  // Structured error context from errors.GetAllDetails()
	PluginVersion string          `json:"plugin_version,omitempty"` // Version of plugin that executed this job
	ParentJobID   string          `json:"parent_job_id,omitempty"`  // For tasks grouped under parent job
	RetryCount    int             `json:"retry_count,omitempty"`    // Number of retry attempts (max 2)

	// TraceContext and TraceBaggage are the job's cause: the span it was
	// enqueued under. A job is enqueued in one context and run later by a
	// worker in another, so the span that caused it is finished by the time it
	// runs; these are what let the execution continue that trace rather than
	// start one of its own. Empty is a job with no cause.
	TraceContext string `json:"trace_context,omitempty"`
	TraceBaggage string `json:"trace_baggage,omitempty"`

	// ExecTraceContext and ExecTraceBaggage are this job's own execution span,
	// left on the row when it starts running so that a child created somewhere
	// the span is not reachable — a plugin, over gRPC, holding only a parent id
	// — has something to hang from. Sentry's headers, in their wire form.
	ExecTraceContext string `json:"exec_trace_context,omitempty"`
	ExecTraceBaggage string `json:"exec_trace_baggage,omitempty"`

	// MadeBy is who made the schedule this job runs for: a caller, or no
	// caller. UserID and Namespace are that caller and where they acted.
	MadeBy    MadeBy `json:"made_by"`
	UserID    string `json:"user_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	Started   Moment    `json:"-"` // When it started, or that it has not; JSON says it as started_at
	Completed Moment    `json:"-"` // When it completed, or that it has not; JSON says it as completed_at
	UpdatedAt time.Time `json:"updated_at"`
}

// MadeBy is who a job, or the schedule that starts it, says made it, as its
// column holds it.
type MadeBy string

const (
	// MadeByCaller is a caller, in a sigil call: the User, and the namespace
	// they acted in, are kept with it.
	MadeByCaller MadeBy = "caller"
	// MadeByNoCaller is no caller: the node, or a plugin itself.
	MadeByNoCaller MadeBy = "no caller"
)

// MadeByOf is who a column says made a job or a schedule. Anything but a
// caller or no caller is refused.
func MadeByOf(said string) (MadeBy, error) {
	switch made := MadeBy(said); made {
	case MadeByCaller, MadeByNoCaller:
		return made, nil
	}
	return "", errors.Newf("made_by is %q: a caller, or no caller", said)
}

// Made is who made the schedule a job runs for: a Caller, or NoCaller.
type Made interface {
	madeTheSchedule()
}

// Caller is the caller whose sigil call made the schedule a job runs for: the
// User, and the namespace they acted in.
type Caller struct {
	UserID    string
	Namespace named.Text
}

// NoCaller is a job no caller made: the node, or a plugin itself.
type NoCaller struct{}

func (Caller) madeTheSchedule()   {}
func (NoCaller) madeTheSchedule() {}

// Made is who made the schedule this job runs for, as it says. A job a caller
// made naming no namespace, or one naming neither, is refused.
func (j *Job) Made() (Made, error) {
	switch j.MadeBy {
	case MadeByCaller:
		namespace, err := named.TextOf("the namespace of the caller of job "+j.ID, j.Namespace)
		if err != nil {
			return nil, errors.Wrapf(err, "job %s (handler %s) was made by a caller", j.ID, j.HandlerName)
		}
		return Caller{UserID: j.UserID, Namespace: namespace}, nil
	case MadeByNoCaller:
		return NoCaller{}, nil
	}
	return nil, errors.Newf("job %s (handler %s) says it was made by %q: a caller, or no caller", j.ID, j.HandlerName, j.MadeBy)
}

// NewJobWithPayload creates a new generic job with handler name and typed payload.
//
// Example:
//
//	payload := BatchImportPayload{SourceURL: "https://...", RecordIDs: []string{"1", "2"}}
//	payloadJSON, _ := json.Marshal(payload)
//	job, _ := async.NewJobWithPayload("data.batch-import", "https://...", payloadJSON, 100)
func NewJobWithPayload(handlerName string, source string, payload json.RawMessage, totalOps int) (*Job, error) {
	return NewChildJobWithPayload(handlerName, source, payload, totalOps, "")
}

// NewChildJobWithPayload creates a new job with an optional parent job ID.
// Use this when creating child jobs that should be grouped under a parent orchestrator job.
func NewChildJobWithPayload(handlerName string, source string, payload json.RawMessage, totalOps int, parentJobID string) (*Job, error) {
	if handlerName == "" {
		err := errors.New("handlerName cannot be empty")
		err = errors.WithDetail(err, "Handler name is required to create a job")
		return nil, err
	}

	jobID, err := identity.GenerateJobID(handlerName, source)
	if err != nil {
		err = errors.Wrap(err, "failed to generate job ASID")
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", handlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", source))
		return nil, err
	}

	now := time.Now()
	return &Job{
		ID:          jobID,
		HandlerName: handlerName,
		Payload:     payload,
		Source:      source,
		Status:      JobStatusQueued,
		Progress:    Progress{Current: 0, Total: totalOps},
		ParentJobID: parentJobID,
		MadeBy:      MadeByNoCaller,
		Pulse:       NoPulse(),
		CreatedAt:   now,
		Started:     NotYet(),
		Completed:   NotYet(),
		UpdatedAt:   now,
	}, nil
}

// Start marks the job as running
func (j *Job) Start() {
	now := time.Now()
	j.Status = JobStatusRunning
	j.Started = At(now)
	j.UpdatedAt = now
}

// Pause marks the job as paused
func (j *Job) Pause(reason string) {
	j.Status = JobStatusPaused
	j.UpdatedAt = time.Now()
	j.Pulse = j.Pulse.paused(true, reason)
}

// Resume marks the job as running again
func (j *Job) Resume() {
	j.Status = JobStatusRunning
	j.UpdatedAt = time.Now()
	j.Pulse = j.Pulse.paused(false, "")
}

// Complete marks the job as completed
func (j *Job) Complete() {
	now := time.Now()
	j.Status = JobStatusCompleted
	j.Completed = At(now)
	j.UpdatedAt = now
}

// Fail marks the job as failed with an error message and preserves structured error details
func (j *Job) Fail(err error) {
	now := time.Now()
	j.Status = JobStatusFailed
	j.Error = err.Error()
	j.ErrorDetails = errors.GetAllDetails(err)
	j.Completed = At(now)
	j.UpdatedAt = now
}

// Cancel marks the job as cancelled with a reason
func (j *Job) Cancel(reason string) {
	now := time.Now()
	j.Status = JobStatusCancelled
	j.Error = reason
	j.Completed = At(now)
	j.UpdatedAt = now
}

// UpdateProgress updates the job's progress
func (j *Job) UpdateProgress(current int) {
	j.Progress.Current = current
	j.UpdatedAt = time.Now()
}

// UpdatePulseState updates the pulse state
func (j *Job) UpdatePulseState(state PulseState) {
	j.Pulse = Pulsed(state)
	j.UpdatedAt = time.Now()
}

// MarshalJSON is a job as the web has always read it: a pulse state, a start
// and a completion only when the job has them. A job naming neither side of
// one is refused.
func (j Job) MarshalJSON() ([]byte, error) {
	type fields Job
	out := struct {
		fields
		PulseState  *PulseState `json:"pulse_state,omitempty"`
		StartedAt   *time.Time  `json:"started_at,omitempty"`
		CompletedAt *time.Time  `json:"completed_at,omitempty"`
	}{fields: fields(j)}

	state, held, err := j.Pulse.State("job " + j.ID)
	if err != nil {
		return nil, err
	}
	if held {
		out.PulseState = &state
	}
	started, startedYet, err := j.Started.When("the start of job " + j.ID)
	if err != nil {
		return nil, err
	}
	if startedYet {
		out.StartedAt = &started
	}
	completed, completedYet, err := j.Completed.When("the completion of job " + j.ID)
	if err != nil {
		return nil, err
	}
	if completedYet {
		out.CompletedAt = &completed
	}
	return json.Marshal(out)
}

// UnmarshalPulseState converts JSON string to PulseState. Emptiness is the
// caller's fact to rule on — a column it knows to be absent is not this
// function's to wave through as a nil state.
func UnmarshalPulseState(data string) (*PulseState, error) {
	if data == "" {
		return nil, errors.New("no pulse state to unmarshal: empty input")
	}
	var state PulseState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		err = errors.Wrap(err, "failed to unmarshal pulse state")
		err = errors.WithDetail(err, fmt.Sprintf("Data length: %d bytes", len(data)))
		return nil, err
	}
	return &state, nil
}
