package server

import (
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
)

// handleGetTaskLogsForJob returns logs for a specific task within a job context
// NEW: Requires both job_id and task_id to avoid ambiguity
// NOTE: task_id may be NULL in database (for stage-level logs), so we also check stage column
func (s *QNTXServer) handleGetTaskLogsForJob(w http.ResponseWriter, r *http.Request, jobID string, taskID string) {
	query := `
		SELECT timestamp, level, message, metadata
		FROM task_logs
		WHERE job_id = ? AND (task_id = ? OR (task_id IS NULL AND stage = ?))
		ORDER BY timestamp ASC
	`

	pulseLog := logger.AddPulseSymbol(s.logger)

	rows, err := s.db.Query(query, jobID, taskID, taskID)
	if err != nil {
		wrappedErr := errors.Wrap(err, "failed to query task logs")
		pulseLog.Errorw("Failed to query task logs",
			"job_id", jobID,
			"task_id", taskID,
			"error", wrappedErr)
		writeError(w, http.StatusInternalServerError, wrappedErr.Error())
		return
	}
	defer rows.Close()

	logs := []LogEntry{}
	for rows.Next() {
		var timestamp, level, message string
		var metadataJSON *string

		if err := rows.Scan(&timestamp, &level, &message, &metadataJSON); err != nil {
			wrappedErr := errors.Wrap(err, "failed to scan task log row")
			pulseLog.Errorw("Failed to scan task log row - database type mismatch or corrupt data",
				"job_id", jobID,
				"task_id", taskID,
				"error", wrappedErr,
				"expected_columns", []string{"timestamp (DATETIME)", "level (TEXT)", "message (TEXT)", "metadata (TEXT/NULL)"})
			writeError(w, http.StatusInternalServerError, wrappedErr.Error())
			return
		}

		var metadata map[string]interface{}
		if metadataJSON != nil {
			if err := json.Unmarshal([]byte(*metadataJSON), &metadata); err != nil {
				pulseLog.Warnw("Failed to unmarshal metadata, using empty object",
					"job_id", jobID,
					"task_id", taskID,
					"error", errors.Wrap(err, "json unmarshal failed"))
				metadata = make(map[string]interface{})
			}
		}

		logs = append(logs, LogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   message,
			Metadata:  metadata,
		})
	}

	writeJSON(w, http.StatusOK, TaskLogsResponse{
		TaskID: taskID,
		Logs:   logs,
	})
}
