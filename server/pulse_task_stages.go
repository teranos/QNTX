// pulse_task_stages.go — GET /api/pulse/jobs/{id}/stages
// Returns stages and tasks for a job, grouped by execution phase with log counts.
package server

import (
	"net/http"

	"github.com/teranos/QNTX/pulse/schedule"
)

// handleGetJobStages returns stages and tasks for a job
func (s *QNTXServer) handleGetJobStages(w http.ResponseWriter, r *http.Request, jobID string) {
	store := schedule.NewTaskLogStore(s.db)

	stages, err := store.ListStagesForJob(jobID)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query task logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, JobStagesResponse{
		JobID:  jobID,
		Stages: stages,
	})
}
