package server

import (
	"fmt"
	"net/http"
)

// handleGetJobStages returns stages and tasks for a job
func (s *QNTXServer) handleGetJobStages(w http.ResponseWriter, r *http.Request, jobID string) {
	// Query task_logs grouped by stage and task_id
	query := `
		SELECT
			COALESCE(stage, 'unknown') as stage,
			COALESCE(task_id, 'unknown') as task_id,
			COUNT(*) as log_count
		FROM task_logs
		WHERE job_id = ?
		GROUP BY stage, task_id
		ORDER BY MIN(id) ASC
	`

	rows, err := s.db.Query(query, jobID)
	if err != nil {
		s.logger.Errorw("Failed to query task logs", "job_id", jobID, "error", err)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query logs: %v", err))
		return
	}
	defer rows.Close()

	// Build stage map
	stageMap := make(map[string][]TaskInfo)
	stageOrder := []string{} // Track order of stages

	for rows.Next() {
		var stage, taskID string
		var logCount int
		if err := rows.Scan(&stage, &taskID, &logCount); err != nil {
			s.logger.Errorw("Failed to scan task log row - database type mismatch or corrupt data",
				"job_id", jobID,
				"error", err,
				"error_detail", err.Error(),
				"expected_columns", []string{"stage (TEXT)", "task_id (TEXT)", "log_count (INTEGER)"},
				"query", "SELECT COALESCE(stage, 'unknown'), COALESCE(task_id, stage), COUNT(*) FROM task_logs",
			)
			continue
		}

		// Track stage order
		if _, exists := stageMap[stage]; !exists {
			stageOrder = append(stageOrder, stage)
			stageMap[stage] = []TaskInfo{}
		}

		stageMap[stage] = append(stageMap[stage], TaskInfo{
			TaskID:   taskID,
			LogCount: logCount,
		})
	}

	// Convert to ordered stages array
	stages := make([]StageInfo, 0, len(stageOrder))
	for _, stage := range stageOrder {
		stages = append(stages, StageInfo{
			Stage: stage,
			Tasks: stageMap[stage],
		})
	}

	writeJSON(w, http.StatusOK, JobStagesResponse{
		JobID:  jobID,
		Stages: stages,
	})
}
