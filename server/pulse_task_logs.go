package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HandlePulseTask handles requests to /api/pulse/tasks/{task_id}
// GET /logs: Get logs for a specific task
func (s *QNTXServer) HandlePulseTask(w http.ResponseWriter, r *http.Request) {
	// Extract task ID from URL path
	// URL format: /api/pulse/tasks/{task_id}/logs
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/pulse/tasks/"), "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		writeError(w, http.StatusBadRequest, "Missing task ID")
		return
	}
	taskID := pathParts[0]

	// Check if this is a request for logs
	if len(pathParts) > 1 && pathParts[1] == "logs" {
		if r.Method == http.MethodGet {
			s.handleGetTaskLogs(w, r, taskID)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	writeError(w, http.StatusNotFound, "Endpoint not found")
}

// handleGetTaskLogs returns all logs for a specific task
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

	rows, err := s.db.Query(query, jobID, taskID, taskID)
	if err != nil {
		s.logger.Errorw("Failed to query task logs",
			"job_id", jobID,
			"task_id", taskID,
			"error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err))
		return
	}
	defer rows.Close()

	logs := []LogEntry{}
	for rows.Next() {
		var timestamp, level, message string
		var metadataJSON *string

		if err := rows.Scan(&timestamp, &level, &message, &metadataJSON); err != nil {
			s.logger.Errorw("Failed to scan task log row - database type mismatch or corrupt data",
				"job_id", jobID,
				"task_id", taskID,
				"error", err,
				"error_detail", err.Error(),
				"expected_columns", []string{"timestamp (DATETIME)", "level (TEXT)", "message (TEXT)", "metadata (TEXT/NULL)"})
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to scan logs: %v", err))
			return
		}

		var metadata map[string]interface{}
		if metadataJSON != nil {
			if err := json.Unmarshal([]byte(*metadataJSON), &metadata); err != nil {
				s.logger.Warnw("Failed to unmarshal metadata, using empty object",
					"job_id", jobID,
					"task_id", taskID,
					"error", err)
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

// handleGetTaskLogs - DEPRECATED: Use handleGetTaskLogsForJob instead
// This endpoint is ambiguous because task_id alone isn't unique across jobs
func (s *QNTXServer) handleGetTaskLogs(w http.ResponseWriter, r *http.Request, taskID string) {
	query := `
		SELECT timestamp, level, message, metadata
		FROM task_logs
		WHERE task_id = ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.Query(query, taskID)
	if err != nil {
		s.logger.Errorw("Failed to query task logs", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err))
		return
	}
	defer rows.Close()

	logs := []LogEntry{}
	for rows.Next() {
		var timestamp, level, message string
		var metadataJSON *string

		if err := rows.Scan(&timestamp, &level, &message, &metadataJSON); err != nil {
			s.logger.Errorw("Failed to scan log row", "error", err)
			continue
		}

		entry := LogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   message,
		}

		// Parse metadata JSON if present
		if metadataJSON != nil && *metadataJSON != "" {
			var metadata map[string]interface{}
			if err := json.Unmarshal([]byte(*metadataJSON), &metadata); err == nil {
				entry.Metadata = metadata
			}
		}

		logs = append(logs, entry)
	}

	writeJSON(w, http.StatusOK, TaskLogsResponse{
		TaskID: taskID,
		Logs:   logs,
	})
}
