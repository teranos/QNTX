package server

import (
	"time"

	"github.com/teranos/QNTX/pulse/schedule"
)

// =======================
// API Request/Response Types
// =======================

// CreateScheduledJobRequest represents the request to create a new scheduled job
type CreateScheduledJobRequest struct {
	ATSCode         string `json:"ats_code"`                   // ATS code to execute (e.g., "ix https://...")
	IntervalSeconds int    `json:"interval_seconds"`           // Execution interval in seconds
	CreatedFromDoc  string `json:"created_from_doc,omitempty"` // Optional: ProseMirror document ID
	Metadata        string `json:"metadata,omitempty"`         // Optional: JSON metadata
	Force           bool   `json:"force,omitempty"`            // Bypass deduplication checks (force execution)
}

// UpdateScheduledJobRequest represents the request to update a scheduled job
type UpdateScheduledJobRequest struct {
	State           *string `json:"state,omitempty"`            // active, paused, stopping, inactive
	IntervalSeconds *int    `json:"interval_seconds,omitempty"` // Update interval
}

// ScheduledJobResponse represents a scheduled job in API responses
type ScheduledJobResponse struct {
	ID              string  `json:"id"`
	ATSCode         string  `json:"ats_code"`
	IntervalSeconds int     `json:"interval_seconds,omitempty"`
	NextRunAt       string  `json:"next_run_at"`                 // RFC3339 timestamp
	LastRunAt       *string `json:"last_run_at,omitempty"`       // RFC3339 timestamp
	LastExecutionID string  `json:"last_execution_id,omitempty"` // Last async job ID
	State           string  `json:"state"`
	CreatedFromDoc  string  `json:"created_from_doc,omitempty"`
	Metadata        string  `json:"metadata,omitempty"`
	CreatedAt       string  `json:"created_at"` // RFC3339 timestamp
	UpdatedAt       string  `json:"updated_at"` // RFC3339 timestamp
}

// ListScheduledJobsResponse represents the response for listing scheduled jobs
type ListScheduledJobsResponse struct {
	Jobs  []ScheduledJobResponse `json:"jobs"`
	Count int                    `json:"count,omitempty"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error string `json:"error"`
}

// TaskInfo represents a task within a stage
type TaskInfo struct {
	TaskID   string `json:"task_id"`
	LogCount int    `json:"log_count,omitempty"`
}

// StageInfo represents a stage with its tasks
type StageInfo struct {
	Stage string     `json:"stage"`
	Tasks []TaskInfo `json:"tasks"`
}

// JobStagesResponse represents the response for GET /jobs/:job_id/stages
type JobStagesResponse struct {
	JobID  string      `json:"job_id"`
	Stages []StageInfo `json:"stages"`
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// TaskLogsResponse represents the response for GET /tasks/:task_id/logs
type TaskLogsResponse struct {
	TaskID string     `json:"task_id"`
	Logs   []LogEntry `json:"logs"`
}

// ChildJobInfo represents a child job summary
type ChildJobInfo struct {
	ID           string  `json:"id"`
	HandlerName  string  `json:"handler_name"`
	Source       string  `json:"source"`
	Status       string  `json:"status"`
	ProgressPct  float64 `json:"progress_pct,omitempty"`
	CostEstimate float64 `json:"cost_estimate,omitempty"`
	CostActual   float64 `json:"cost_actual,omitempty"`
	Error        string  `json:"error,omitempty"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    *string `json:"started_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
}

// JobChildrenResponse represents the response for GET /api/pulse/jobs/:id/children
type JobChildrenResponse struct {
	ParentJobID string         `json:"parent_job_id"`
	Children    []ChildJobInfo `json:"children"`
}

// =======================
// Helper Functions
// =======================

// toScheduledJobResponse converts a schedule.Job to API response format
func toScheduledJobResponse(job *schedule.Job) ScheduledJobResponse {
	resp := ScheduledJobResponse{
		ID:              job.ID,
		ATSCode:         job.ATSCode,
		IntervalSeconds: job.IntervalSeconds,
		NextRunAt:       job.NextRunAt.Format(time.RFC3339),
		LastExecutionID: job.LastExecutionID,
		State:           job.State,
		CreatedFromDoc:  job.CreatedFromDoc,
		Metadata:        job.Metadata,
		CreatedAt:       job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       job.UpdatedAt.Format(time.RFC3339),
	}

	if job.LastRunAt != nil {
		lastRun := job.LastRunAt.Format(time.RFC3339)
		resp.LastRunAt = &lastRun
	}

	return resp
}

// Note: writeJSON and writeError functions moved to response.go for DRY
