// pulse_job_children.go — GET /api/pulse/jobs/{id}/children
// Returns child async jobs spawned by a parent scheduled job's most recent execution.
package server

import (
	"net/http"
	"time"

	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
)

// handleGetJobChildren returns all child jobs for a given parent job
func (s *QNTXServer) handleGetJobChildren(w http.ResponseWriter, r *http.Request, scheduledJobID string) {
	// The incoming ID is a scheduled job ID (SP...), but child tasks are linked to async job IDs (JB...).
	// We need to find the most recent execution's async_job_id for this scheduled job.
	execStore := schedule.NewExecutionStore(s.db)

	asyncJobID, err := execStore.GetAsyncJobIDForScheduledJob(scheduledJobID)
	if err != nil {
		logger.AddPulseSymbol(s.logger).Errorw("Failed to find async job for scheduled job",
			"scheduled_job_id", scheduledJobID,
			"error", err)
		writeWrappedError(w, s.logger, err, "failed to find async job for scheduled job", http.StatusInternalServerError)
		return
	}

	if asyncJobID == "" {
		// No executions yet for this scheduled job - return empty list
		writeJSON(w, http.StatusOK, JobChildrenResponse{
			ParentJobID: scheduledJobID,
			Children:    []ChildJobInfo{},
		})
		return
	}

	// Fetch child jobs using the async job ID
	queue := async.NewQueue(s.db)
	childJobs, err := queue.ListTasksByParent(asyncJobID)
	if err != nil {
		logger.AddPulseSymbol(s.logger).Errorw("Failed to fetch child jobs",
			"scheduled_job_id", scheduledJobID,
			"async_job_id", asyncJobID,
			"error", err)
		writeWrappedError(w, s.logger, err, "failed to fetch child jobs", http.StatusInternalServerError)
		return
	}

	// Convert to response format
	children := make([]ChildJobInfo, 0, len(childJobs))
	for _, job := range childJobs {
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
