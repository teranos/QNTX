package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/teranos/QNTX/pulse/async"
)

// handleGetJobChildren returns all child jobs for a given parent job
func (s *QNTXServer) handleGetJobChildren(w http.ResponseWriter, r *http.Request, scheduledJobID string) {
	// The incoming ID is a scheduled job ID (SP...), but child tasks are linked to async job IDs (JB...).
	// We need to find the most recent execution's async_job_id for this scheduled job.

	var asyncJobID string
	err := s.db.QueryRow(`
		SELECT async_job_id
		FROM pulse_executions
		WHERE scheduled_job_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, scheduledJobID).Scan(&asyncJobID)

	if err != nil {
		if err == sql.ErrNoRows {
			// No executions yet for this scheduled job - return empty list
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"children": []ChildJobInfo{},
			})
			return
		}
		s.logger.Errorw("Failed to find async job for scheduled job",
			"scheduled_job_id", scheduledJobID,
			"error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to find async job: %v", err))
		return
	}

	// Get the async job queue
	queue := async.NewQueue(s.db)

	// Fetch child jobs using the async job ID
	childJobs, err := queue.ListTasksByParent(asyncJobID)
	if err != nil {
		s.logger.Errorw("Failed to fetch child jobs",
			"scheduled_job_id", scheduledJobID,
			"async_job_id", asyncJobID,
			"error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to fetch child jobs: %v", err))
		return
	}

	// Convert to response format
	children := make([]ChildJobInfo, 0, len(childJobs))
	for _, job := range childJobs {
		// Calculate progress percentage
		var progressPct float64
		if job.Progress.Total > 0 {
			progressPct = float64(job.Progress.Current) / float64(job.Progress.Total) * 100
		}

		child := ChildJobInfo{
			ID:           job.ID,
			HandlerName:  job.HandlerName,
			Source:       job.Source,
			Status:       string(job.Status),
			ProgressPct:  progressPct,
			CostEstimate: job.CostEstimate,
			CostActual:   job.CostActual,
			CreatedAt:    job.CreatedAt.Format(time.RFC3339),
		}

		if job.Error != "" {
			child.Error = job.Error
		}

		if job.StartedAt != nil {
			started := job.StartedAt.Format(time.RFC3339)
			child.StartedAt = &started
		}

		if job.CompletedAt != nil {
			completed := job.CompletedAt.Format(time.RFC3339)
			child.CompletedAt = &completed
		}

		children = append(children, child)
	}

	writeJSON(w, http.StatusOK, JobChildrenResponse{
		ParentJobID: asyncJobID,
		Children:    children,
	})
}
