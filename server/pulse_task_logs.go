// pulse_task_logs.go — GET /api/pulse/jobs/{id}/tasks/{task_id}/logs
// Returns log entries for a specific task within a job context.
package server

import (
	"net/http"

	"github.com/teranos/QNTX/pulse/schedule"
)

// handleGetTaskLogsForJob returns logs for a specific task within a job context.
// task_id may be NULL in database (for stage-level logs), so the store also checks the stage column.
func (s *QNTXServer) handleGetTaskLogsForJob(w http.ResponseWriter, r *http.Request, jobID string, taskID string) {
	store := schedule.NewTaskLogStore(s.db)

	logs, err := store.ListLogsForTask(jobID, taskID)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query task logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, TaskLogsResponse{
		TaskID: taskID,
		Logs:   logs,
	})
}
