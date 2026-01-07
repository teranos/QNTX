package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	id "github.com/teranos/vanity-id"
)

// HandlePulseSchedules handles requests to /api/pulse/schedules
// GET: List all schedules
// POST: Create a new schedule
func (s *QNTXServer) HandlePulseSchedules(w http.ResponseWriter, r *http.Request) {
	endpoint := "unknown"
	switch r.Method {
	case http.MethodGet:
		endpoint = "list jobs"
	case http.MethodPost:
		endpoint = "create job"
	}

	logger.AddPulseSymbol(s.logger).Infow("Pulse "+endpoint,
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr)

	switch r.Method {
	case http.MethodGet:
		s.handleListSchedules(w, r)
	case http.MethodPost:
		s.handleCreateSchedule(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandlePulseSchedule handles requests to /api/pulse/schedules/{id}
// GET: Get schedule details
// PATCH: Update schedule (pause/resume/change interval)
// DELETE: Remove schedule
func (s *QNTXServer) HandlePulseSchedule(w http.ResponseWriter, r *http.Request) {
	// Extract schedule ID from URL path
	// URL format: /api/pulse/schedules/{id} or /api/pulse/schedules/{id}/executions
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/pulse/schedules/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID")
		return
	}
	jobID := pathParts[0]

	// Check if this is a request for executions (schedule execution history)
	if len(pathParts) > 1 && pathParts[1] == "executions" {
		logger.AddPulseSymbol(s.logger).Infow("Pulse get executions", "schedule_id", jobID)
		s.HandleJobExecutions(w, r, jobID)
		return
	}

	// Log specific action for single job operations
	endpoint := "unknown"
	switch r.Method {
	case http.MethodGet:
		endpoint = "get job"
	case http.MethodPatch:
		endpoint = "update job"
	case http.MethodDelete:
		endpoint = "delete job"
	}
	logger.AddPulseSymbol(s.logger).Infow("Pulse "+endpoint, "job_id", jobID, "method", r.Method)

	switch r.Method {
	case http.MethodGet:
		s.handleGetSchedule(w, r, jobID)
	case http.MethodPatch:
		s.handleUpdateSchedule(w, r, jobID)
	case http.MethodDelete:
		s.handleDeleteSchedule(w, r, jobID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleListSchedules lists all schedules
func (s *QNTXServer) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	// List all scheduled jobs regardless of state (active, paused, inactive, etc.)
	jobs, err := s.getScheduleStore().ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs", "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list jobs: %v", err))
		return
	}

	response := ListScheduledJobsResponse{
		Jobs:  make([]ScheduledJobResponse, 0, len(jobs)),
		Count: len(jobs),
	}

	for _, job := range jobs {
		response.Jobs = append(response.Jobs, toScheduledJobResponse(job))
	}

	writeJSON(w, http.StatusOK, response)
}

// handleCreateSchedule creates a new schedule
func (s *QNTXServer) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	pulseLog := logger.AddPulseSymbol(s.logger)

	var req CreateScheduledJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		pulseLog.Warnw("Pulse create job - invalid JSON",
			"error", err,
			"remote", r.RemoteAddr)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	pulseLog.Infow("Pulse create job request",
		"ats_code", req.ATSCode,
		"interval_seconds", req.IntervalSeconds,
		"force", req.Force,
		"created_from_doc", req.CreatedFromDoc,
		"remote", r.RemoteAddr)

	// Validate request
	if req.ATSCode == "" {
		pulseLog.Warnw("Pulse create job - missing ats_code")
		writeError(w, http.StatusBadRequest, "ats_code is required")
		return
	}
	// Allow interval_seconds = 0 for one-time force trigger executions
	// Force flag indicates this is a one-time run that bypasses deduplication
	if req.IntervalSeconds < 0 {
		pulseLog.Warnw("Pulse create job - invalid interval",
			"interval_seconds", req.IntervalSeconds)
		writeError(w, http.StatusBadRequest, "interval_seconds cannot be negative")
		return
	}
	if req.IntervalSeconds == 0 && !req.Force {
		pulseLog.Warnw("Pulse create job - zero interval without force",
			"interval_seconds", req.IntervalSeconds,
			"force", req.Force)
		writeError(w, http.StatusBadRequest, "interval_seconds must be positive for recurring jobs (use force=true for one-time execution)")
		return
	}

	// Generate job ID using ASID format
	jobID, err := id.GenerateASID(req.ATSCode, "scheduled", "pulse", "system")
	if err != nil {
		pulseLog.Errorw("Pulse create job - ID generation failed",
			"error", err,
			"ats_code", req.ATSCode)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to generate job ID: %v", err))
		return
	}

	// Parse ATS code to pre-compute handler, payload, and source URL
	// This validates the ATS code format and makes the ticker domain-agnostic
	parsed, err := ParseATSCodeWithForce(req.ATSCode, jobID, req.Force)
	if err != nil {
		pulseLog.Warnw("Pulse create job - invalid ATS code",
			"error", err,
			"ats_code", req.ATSCode,
			"job_id", jobID)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid ATS code: %v", err))
		return
	}

	pulseLog.Infow("Pulse create job - parsed",
		"job_id", jobID,
		"handler_name", parsed.HandlerName,
		"source_url", parsed.SourceURL,
		"force", req.Force)

	now := time.Now()

	// Force trigger: Enqueue async job directly instead of creating scheduled job
	if req.Force && req.IntervalSeconds == 0 {
		pulseLog.Infow("Force trigger - preparing tracking and async job",
			"job_id", jobID,
			"handler_name", parsed.HandlerName,
			"source_url", parsed.SourceURL)

		// CRITICAL: Create all tracking records BEFORE enqueueing async job
		// Use transaction to prevent race conditions (job deletion between SELECT and INSERT)

		// Step 1: Find or create scheduled job for tracking (with transaction)
		var scheduledJobID string
		var executionID string

		// Start transaction for atomic job lookup/creation and execution record insert
		tx, err := s.db.Begin()
		if err != nil {
			s.logger.Errorw("Failed to begin transaction for force trigger",
				"error", err,
				"ats_code", req.ATSCode)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to begin transaction: %v", err))
			return
		}
		defer tx.Rollback() // Rollback if not committed

		// Try to find active scheduled job
		err = tx.QueryRow(`SELECT id FROM scheduled_pulse_jobs WHERE ats_code = ? AND state = 'active' LIMIT 1`,
			req.ATSCode).Scan(&scheduledJobID)

		// If no active scheduled job found, check for existing temp job and reuse or create new one
		if err != nil || scheduledJobID == "" {
			// Try to find existing temp job for this ATS code (prevents proliferation)
			err = tx.QueryRow(`SELECT id FROM scheduled_pulse_jobs WHERE ats_code = ? AND created_from_doc = '__force_trigger__' ORDER BY created_at DESC LIMIT 1`,
				req.ATSCode).Scan(&scheduledJobID)

			if err != nil || scheduledJobID == "" {
				// No temp job exists - create temporary scheduled job for tracking
				scheduledJobID, err = id.GenerateASID(req.ATSCode, "force-trigger", "pulse", "system")
				if err != nil {
					s.logger.Errorw("Failed to generate temp job ID for force trigger",
						"error", err,
						"ats_code", req.ATSCode)
					writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to generate tracking job ID: %v", err))
					return
				}

				_, err = tx.Exec(`
					INSERT INTO scheduled_pulse_jobs (id, ats_code, handler_name, payload, source_url, state, interval_seconds, created_at, updated_at, created_from_doc)
					VALUES (?, ?, ?, ?, ?, 'inactive', 0, ?, ?, '__force_trigger__')
				`, scheduledJobID, req.ATSCode, parsed.HandlerName, parsed.Payload, parsed.SourceURL, now.Format(time.RFC3339), now.Format(time.RFC3339))

				if err != nil {
					s.logger.Errorw("Failed to create temp scheduled job for force trigger",
						"error", err,
						"scheduled_job_id", scheduledJobID,
						"ats_code", req.ATSCode)
					writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create tracking job: %v", err))
					return
				}

				pulseLog.Infow("Created temp scheduled job for force trigger",
					"scheduled_job_id", scheduledJobID,
					"ats_code", req.ATSCode)
			} else {
				pulseLog.Infow("Reusing existing temp job for force trigger",
					"scheduled_job_id", scheduledJobID,
					"ats_code", req.ATSCode)
			}
		}

		// Step 2: Create async job (but don't enqueue yet)
		asyncJob, err := async.NewJobWithPayload(
			parsed.HandlerName,
			parsed.SourceURL,
			parsed.Payload,
			0,   // Total unknown
			0.0, // Cost calculated during execution
			fmt.Sprintf("user:force-trigger:%s", jobID),
		)
		if err != nil {
			s.logger.Errorw("Failed to create async job for force trigger",
				"error", err,
				"handler", parsed.HandlerName)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create async job: %v", err))
			return
		}

		// Step 3: Create execution record (within same transaction - guaranteed FK integrity)
		executionID = id.GenerateExecutionID()

		_, err = tx.Exec(`
			INSERT INTO pulse_executions (id, scheduled_job_id, async_job_id, status, started_at, created_at, updated_at)
			VALUES (?, ?, ?, 'running', ?, ?, ?)
		`, executionID, scheduledJobID, asyncJob.ID, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			s.logger.Errorw("Failed to create pulse_execution record for force trigger",
				"error", err,
				"execution_id", executionID,
				"async_job_id", asyncJob.ID)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create execution record: %v", err))
			return
		}

		// Commit transaction - all tracking records now atomically created
		if err = tx.Commit(); err != nil {
			s.logger.Errorw("Failed to commit force trigger transaction",
				"error", err,
				"execution_id", executionID)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to commit transaction: %v", err))
			return
		}

		pulseLog.Infow("Created pulse_execution for force trigger",
			"execution_id", executionID,
			"async_job_id", asyncJob.ID,
			"scheduled_job_id", scheduledJobID)

		// Step 4: NOW enqueue async job (all tracking is in place)
		queue := s.daemon.GetQueue()
		if err := queue.Enqueue(asyncJob); err != nil {
			s.logger.Errorw("Failed to enqueue force trigger job",
				"error", err,
				"job_id", asyncJob.ID)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to enqueue job: %v", err))
			return
		}

		pulseLog.Infow("Force trigger enqueued",
			"async_job_id", asyncJob.ID,
			"handler_name", parsed.HandlerName,
			"source_url", parsed.SourceURL)

		// Return success - all operations completed
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"id":           scheduledJobID, // Return scheduled job ID for UI tracking
			"async_job_id": asyncJob.ID,
			"handler_name": parsed.HandlerName,
			"source_url":   parsed.SourceURL,
			"force":        true,
		})
		return
	}

	// Regular scheduled job creation
	job := &schedule.Job{
		ID:              jobID,
		ATSCode:         req.ATSCode,
		HandlerName:     parsed.HandlerName,
		Payload:         parsed.Payload,
		SourceURL:       parsed.SourceURL,
		IntervalSeconds: req.IntervalSeconds,
		NextRunAt:       now, // Run immediately on first execution
		State:           schedule.StateActive,
		CreatedFromDoc:  req.CreatedFromDoc,
		Metadata:        req.Metadata,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.getScheduleStore().CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create scheduled job", "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create job: %v", err))
		return
	}

	pulseLog.Infow("Created scheduled job",
		"job_id", jobID,
		"ats_code", req.ATSCode,
		"interval_seconds", req.IntervalSeconds)

	writeJSON(w, http.StatusCreated, toScheduledJobResponse(job))
}

// handleGetSchedule retrieves a specific schedule
func (s *QNTXServer) handleGetSchedule(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := s.getScheduleStore().GetJob(jobID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, fmt.Sprintf("Job not found: %s", jobID))
			return
		}
		s.logger.Errorw("Failed to get scheduled job", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get job: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, toScheduledJobResponse(job))
}

// handleUpdateSchedule updates a schedule (pause/resume/change interval)
func (s *QNTXServer) handleUpdateSchedule(w http.ResponseWriter, r *http.Request, jobID string) {
	var req UpdateScheduledJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	// Update state if provided
	if req.State != nil {
		validStates := map[string]bool{
			schedule.StateActive:   true,
			schedule.StatePaused:   true,
			schedule.StateStopping: true,
			schedule.StateInactive: true,
		}
		if !validStates[*req.State] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid state: %s", *req.State))
			return
		}

		if err := s.getScheduleStore().UpdateJobState(jobID, *req.State); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, fmt.Sprintf("Job not found: %s", jobID))
				return
			}
			s.logger.Errorw("Failed to update job state", "job_id", jobID, "error", err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update job: %v", err))
			return
		}

		logger.AddPulseSymbol(s.logger).Infow("Updated scheduled job state",
			"job_id", jobID,
			"new_state", *req.State)
	}

	// Handle interval_seconds update
	if req.IntervalSeconds != nil {
		if *req.IntervalSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "interval_seconds must be positive")
			return
		}

		if err := s.getScheduleStore().UpdateJobInterval(jobID, *req.IntervalSeconds); err != nil {
			s.logger.Errorw("Failed to update job interval", "job_id", jobID, "error", err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update interval: %v", err))
			return
		}

		logger.AddPulseSymbol(s.logger).Infow("Updated scheduled job interval",
			"job_id", jobID,
			"new_interval", *req.IntervalSeconds)
	}

	// Return updated job
	job, err := s.getScheduleStore().GetJob(jobID)
	if err != nil {
		s.logger.Errorw("Failed to get updated job", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get updated job: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, toScheduledJobResponse(job))
}

// handleDeleteSchedule removes a schedule and cancels its async tasks
func (s *QNTXServer) handleDeleteSchedule(w http.ResponseWriter, r *http.Request, jobID string) {
	// Get job details before deletion for logging
	job, err := s.getScheduleStore().GetJob(jobID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, fmt.Sprintf("Job not found: %s", jobID))
			return
		}
		s.logger.Errorw("Failed to get job for deletion", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get job: %v", err))
		return
	}

	// Find the most recent async job execution for this scheduled job
	execStore := schedule.NewExecutionStore(s.db)
	executions, _, err := execStore.ListExecutions(jobID, 1, 0, "") // Get most recent execution
	if err != nil {
		s.logger.Warnw("Failed to get executions for cascade deletion", "job_id", jobID, "error", err)
		// Continue with deletion even if we can't find executions
	} else if len(executions) > 0 && executions[0].AsyncJobID != nil {
		// Delete the async job and all its child tasks
		asyncJobID := *executions[0].AsyncJobID
		queue := async.NewQueue(s.db)
		if err := queue.DeleteJobWithChildren(asyncJobID); err != nil {
			s.logger.Warnw("Failed to cascade delete async job", "job_id", jobID, "async_job_id", asyncJobID, "error", err)
			// Continue with scheduled job deletion even if cascade fails
		} else {
			logger.AddPulseSymbol(s.logger).Infow("Cascade cancellation of job", "async_job_id", asyncJobID)
		}
	}

	// Set job to deleted state (soft delete)
	// We don't hard delete to preserve execution history
	if err := s.getScheduleStore().UpdateJobState(jobID, schedule.StateDeleted); err != nil {
		s.logger.Errorw("Failed to delete job", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete job: %v", err))
		return
	}

	logger.AddPulseSymbol(s.logger).Infow("Deleted scheduled job",
		"job_id", jobID,
		"ats_code", job.ATSCode,
		"interval_seconds", job.IntervalSeconds)

	w.WriteHeader(http.StatusNoContent) // 204 No Content
}

// getScheduleStore returns the schedule store for database operations
func (s *QNTXServer) getScheduleStore() *schedule.Store {
	return schedule.NewStore(s.db)
}
