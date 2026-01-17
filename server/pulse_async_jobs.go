package server

import (
	"net/http"

	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
)

const (
	// Default and max limits for job listing queries
	defaultJobLimit = 50
	maxJobLimit     = 200
)

// HandlePulseJobs handles requests to /api/pulse/jobs
// GET: List all async jobs (active, completed, failed)
func (s *QNTXServer) HandlePulseJobs(w http.ResponseWriter, r *http.Request) {
	logger.AddPulseSymbol(s.logger).Infow("Pulse list async jobs",
		"method", r.Method,
		"path", r.URL.Path,
		"remote", r.RemoteAddr)

	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	s.handleListAsyncJobs(w, r)
}

// HandlePulseJob handles requests to /api/pulse/jobs/{id}
// GET: Get async job details
// Sub-resources: /api/pulse/jobs/{id}/children, /api/pulse/jobs/{id}/stages, /api/pulse/jobs/{id}/tasks/:task_id/logs
func (s *QNTXServer) HandlePulseJob(w http.ResponseWriter, r *http.Request) {
	// Extract job ID from URL path
	pathParts := extractPathParts(r.URL.Path, "/api/pulse/jobs/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		writeError(w, http.StatusBadRequest, "Missing job ID")
		return
	}
	jobID := pathParts[0]

	// Check if this is a request for child jobs
	if len(pathParts) > 1 && pathParts[1] == "children" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		logger.AddPulseSymbol(s.logger).Infow("Pulse get children", "job_id", jobID)
		s.handleGetJobChildren(w, r, jobID)
		return
	}

	// Check if this is a request for stages
	if len(pathParts) > 1 && pathParts[1] == "stages" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		logger.AddPulseSymbol(s.logger).Infow("Pulse get stages", "job_id", jobID)
		s.handleGetJobStages(w, r, jobID)
		return
	}

	// Check if this is a request for task logs: /api/pulse/jobs/:job_id/tasks/:task_id/logs
	if len(pathParts) >= 4 && pathParts[1] == "tasks" && pathParts[3] == "logs" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		taskID := pathParts[2]
		logger.AddPulseSymbol(s.logger).Infow("Pulse get task logs", "job_id", jobID, "task_id", taskID)
		s.handleGetTaskLogsForJob(w, r, jobID, taskID)
		return
	}

	// Handle single async job operations
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	s.handleGetAsyncJob(w, r, jobID)
}

// handleListAsyncJobs lists all async jobs (active + completed + failed)
func (s *QNTXServer) handleListAsyncJobs(w http.ResponseWriter, r *http.Request) {
	// Check if daemon is available
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "Daemon not available")
		return
	}

	queue := s.daemon.GetQueue()

	// Parse limit from query params
	limit := parseIntQueryParam(r, "limit", defaultJobLimit, 1, maxJobLimit)

	// Fetch all job types
	var allJobs []*async.Job

	// Active jobs (queued, running, paused)
	activeJobs, err := queue.ListActiveJobs(limit)
	if err != nil {
		s.logger.Warnw("Failed to list active jobs", "error", err)
	} else {
		allJobs = append(allJobs, activeJobs...)
	}

	// Completed jobs
	completedJobs, err := queue.ListJobs(asyncJobStatusPtr(async.JobStatusCompleted), limit)
	if err != nil {
		s.logger.Warnw("Failed to list completed jobs", "error", err)
	} else {
		allJobs = append(allJobs, completedJobs...)
	}

	// Failed jobs
	failedJobs, err := queue.ListJobs(asyncJobStatusPtr(async.JobStatusFailed), limit)
	if err != nil {
		s.logger.Warnw("Failed to list failed jobs", "error", err)
	} else {
		allJobs = append(allJobs, failedJobs...)
	}

	// Return jobs as JSON
	response := map[string]interface{}{
		"jobs":  allJobs,
		"count": len(allJobs),
	}

	writeJSON(w, http.StatusOK, response)
}

// handleGetAsyncJob retrieves a specific async job by ID
func (s *QNTXServer) handleGetAsyncJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if s.daemon == nil {
		writeError(w, http.StatusServiceUnavailable, "Daemon not available")
		return
	}

	queue := s.daemon.GetQueue()
	job, err := queue.GetJob(jobID)
	if err != nil {
		handleError(w, s.logger, err, "failed to get async job")
		return
	}

	writeJSON(w, http.StatusOK, job)
}
